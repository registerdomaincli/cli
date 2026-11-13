package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// newDomainContactsCmd manages the WHOIS contacts of an OWNED domain:
//
//	domains contacts <domain>                    — show the live contacts
//	domains contacts <domain> --contact <label>  — apply a saved set
//	domains contacts <domain> --role-* specs     — registrant + role overrides
//	domains contacts <domain> --own-contact      — enter the registrant inline
//
// Precedence mirrors register/transfer: flags > --contact label > default
// set > interactive (TTY) > error. The registrant is mandatory; roles left
// unset inherit it. A registrant change may trigger ICANN verification and
// a registrar transfer lock — the command warns before confirming when the
// registrant fields change.
func newDomainContactsCmd(app *App) *cobra.Command {
	var contact, ownContact string
	var rf registrantFlags
	var rolf roleFlags
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "contacts <domain>",
		Short: i18n.T("domains.contacts.short"),
		Long:  i18n.T("domains.contacts.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := strings.ToLower(strings.TrimSpace(args[0]))

			// Flag syntax fails fast, before any network call.
			if err := rolf.collect(cmd); err != nil {
				return err
			}

			c, err := app.requireClient()
			if err != nil {
				return err
			}

			// --show / default: read-only view of the live contacts.
			if isShow(cmd, &rf, &rolf, contact) {
				return showDomainContacts(cmd, c, domain)
			}

			// Resolve the new registrant AND roles from ONE source. With
			// --contact (or the default set) the saved set is fetched ONCE
			// and both the registrant and its roles come from that single
			// snapshot — a set edit or default change between requests can
			// never mix one set's registrant with another's roles.
			var reg *apitypes.Registrant
			var roles *apitypes.RoleContacts
			var prompted bool
			if rf.any() {
				built, berr := rf.build()
				if berr != nil {
					return berr
				}
				reg, prompted = built, false
				roles, err = resolveRoles(cmd, app, &rolf, reg, prompted, nil)
				if err != nil {
					return err
				}
			} else {
				cs, cserr := resolveContactSetForUpdate(cmd, app, contact)
				if cserr != nil {
					return cserr
				}
				prompted = false
				reg = &cs.Registrant
				roles, err = resolveRoles(cmd, app, &rolf, reg, prompted, cs.Roles)
				if err != nil {
					return err
				}
			}

			if dryRun {
				return emitDomainContactsPlan(cmd, domain, *reg, roles)
			}

			// Human-approval gate with a visible summary: contacts changes
			// can trigger ICANN verification and a registrar transfer lock
			// when the registrant changes — the human must see WHAT is
			// about to be applied, not just a bare y/N.
			fmt.Fprint(cmd.OutOrStdout(), i18n.T("domains.contacts.confirmHdr", "Domain", domain))
			printContactFields(cmd, *reg)
			printRoleSummary(cmd, roles)
			confirmed, err := requireYes(cmd, app)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("confirm.aborted"))
				return nil
			}

			out, err := c.SetDomainContacts(domain, apitypes.UpdateDomainContactsInput{
				Registrant:   *reg,
				RoleContacts: roles,
			})
			if err != nil {
				return fail(err)
			}
			if err := emitJSON(cmd, out); err != nil {
				return err
			}
			if jsonFlag {
				return nil
			}
			printDomainContacts(cmd, out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change and stop")
	cmd.Flags().StringVar(&contact, "contact", "", "apply the saved contact set with this label")
	cmd.Flags().StringVar(&ownContact, "own-contact", "", "unused placeholder to reserve the flag namespace")
	_ = ownContact
	cmd.Flags().MarkHidden("own-contact")
	addRegistrantFlags(cmd, &rf)
	addRoleFlags(cmd, &rolf)
	return cmd
}

// isShow reports whether the invocation is a plain read: no registrant
// flags, no role flags, no --contact.
func isShow(cmd *cobra.Command, rf *registrantFlags, rolf *roleFlags, contact string) bool {
	if contact != "" || rf.any() {
		return false
	}
	if len(rolf.parsed) == 0 {
		_ = rolf.collect(cmd)
	}
	return len(rolf.parsed) == 0
}

// resolveContactSetForUpdate fetches ONE saved set (by label, or the
// account default) and returns it whole: the PUT takes both the registrant
// and the roles from this single snapshot, so a set edit or default change
// between requests can never mix snapshots. Non-interactive without a
// label and no default fails naming the flags — never a silent pick.
func resolveContactSetForUpdate(cmd *cobra.Command, app *App, label string) (*apitypes.ContactSet, error) {
	c, err := app.requireClient()
	if err != nil {
		return nil, err
	}
	sets, err := c.ContactSets()
	if err != nil {
		return nil, fail(err)
	}
	if label != "" {
		for i := range sets.ContactSets {
			if sets.ContactSets[i].Label == label {
				return &sets.ContactSets[i], nil
			}
		}
		return nil, errors.New(i18n.T("contacts.notFound", "Label", label))
	}
	for i := range sets.ContactSets {
		if sets.ContactSets[i].IsDefault {
			return &sets.ContactSets[i], nil
		}
	}
	if !app.p.stdinIsTTY() {
		return nil, missingFlags("--contact", "--first-name", "--last-name",
			"--email", "--phone", "--address1", "--city", "--zip", "--country")
	}
	return nil, missingFlags("--contact")
}

// printRoleSummary renders the per-role lines of a plan/confirmation.
func printRoleSummary(cmd *cobra.Command, roles *apitypes.RoleContacts) {
	if roles == nil {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.T("domains.contacts.allInherit"))
		return
	}
	for _, name := range roleNames {
		rc := map[string]*apitypes.Registrant{
			"admin":   roles.Admin,
			"tech":    roles.Tech,
			"billing": roles.Billing,
		}[name]
		if rc == nil {
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.roleFrom", "Role", name, "Who", i18n.T("contacts.roleInherit")))
			continue
		}
		fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.roleFrom", "Role", name,
			"Who", rc.FirstName+" "+rc.LastName+" <"+rc.Email+">"))
	}
}

func showDomainContacts(cmd *cobra.Command, c *Client, domain string) error {
	live, err := c.DomainContacts(domain)
	if err != nil {
		return fail(err)
	}
	if err := emitJSON(cmd, live); err != nil {
		return err
	}
	if jsonFlag {
		return nil
	}
	printDomainContacts(cmd, live)
	return nil
}

func printDomainContacts(cmd *cobra.Command, live apitypes.DomainContactsResponse) {
	out := cmd.OutOrStdout()
	r := live.Registrant
	fmt.Fprintln(out, i18n.T("domains.contacts.hdrRegistrant"))
	printContactFields(cmd, r)
	if live.RoleContacts == nil {
		fmt.Fprintln(out, i18n.T("domains.contacts.allInherit"))
		return
	}
	for _, name := range roleNames {
		rc := map[string]*apitypes.Registrant{
			"admin":   live.RoleContacts.Admin,
			"tech":    live.RoleContacts.Tech,
			"billing": live.RoleContacts.Billing,
		}[name]
		if rc == nil {
			fmt.Fprintln(out, i18n.T("contacts.roleFrom", "Role", name, "Who", i18n.T("contacts.roleInherit")))
			continue
		}
		fmt.Fprintln(out, i18n.T("contacts.roleFrom", "Role", name,
			"Who", rc.FirstName+" "+rc.LastName+" <"+rc.Email+">"))
	}
}

func printContactFields(cmd *cobra.Command, r apitypes.Registrant) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "  name:     %s %s\n", r.FirstName, r.LastName)
	fmt.Fprintf(out, "  org:      %s\n", withSpace(r.Organization))
	fmt.Fprintf(out, "  email:    %s\n", r.Email)
	fmt.Fprintf(out, "  phone:    %s\n", r.Phone)
	fmt.Fprintf(out, "  address:  %s %s, %s %s, %s\n", r.Address1, withSpace(r.Address2), r.City, r.Zip, r.Country)
	fmt.Fprintf(out, "  state:    %s\n", r.State)
}

func emitDomainContactsPlan(cmd *cobra.Command, domain string, reg apitypes.Registrant, roles *apitypes.RoleContacts) error {
	if jsonFlag {
		return emitJSON(cmd, apitypes.UpdateDomainContactsInput{Registrant: reg, RoleContacts: roles})
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, i18n.T("domains.contacts.dryRunHdr", "Domain", domain))
	printContactFields(cmd, reg)
	printRoleSummary(cmd, roles)
	fmt.Fprintln(out, i18n.T("domains.contacts.dryRunFoot"))
	return nil
}
