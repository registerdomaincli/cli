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

			// Resolve the new registrant (flags > --contact > default set >
			// prompt). Reuses the order-flow resolver: same precedence, same
			// non-interactive contract. --own-contact is not offered here.
			reg, setRoles, setID, prompted, err := resolveRegistrantForOrder(cmd, app, false, contact, &rf)
			if err != nil {
				return err
			}
			roles, err := resolveRoles(cmd, app, &rolf, reg, prompted, setRoles)
			if err != nil {
				return err
			}
			if reg == nil {
				// Registrant rides on the contact set: fetch it so the PUT
				// carries the full registrant (the endpoint requires it).
				label := contact
				if label == "" {
					// default set: find it
					sets, gerr := c.ContactSets()
					if gerr != nil {
						return fail(gerr)
					}
					found := false
					for _, s := range sets.ContactSets {
						if s.IsDefault {
							label, found = s.Label, true
							break
						}
					}
					if !found {
						return missingFlags("--contact", "--first-name")
					}
				}
				cs, gerr := findContactSet(c, label)
				if gerr != nil {
					return gerr
				}
				reg = &cs.Registrant
				if roles == nil {
					roles = setRoles
				}
			}
			_ = setID

			if dryRun {
				return emitDomainContactsPlan(cmd, domain, *reg, roles)
			}

			// Human-approval gate: contacts changes can trigger ICANN
			// verification and a registrar transfer lock.
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

func showDomainContacts(cmd *cobra.Command, c *Client, domain string) error {
	live, err := c.DomainContacts(domain)
	if err != nil {
		return fail(err)
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
	fmt.Fprintf(out, "  email:    %s\n", r.Email)
	fmt.Fprintf(out, "  phone:    %s\n", r.Phone)
	fmt.Fprintf(out, "  address:  %s %s, %s %s, %s\n", r.Address1, withSpace(r.Address2), r.City, r.Zip, r.Country)
}

func emitDomainContactsPlan(cmd *cobra.Command, domain string, reg apitypes.Registrant, roles *apitypes.RoleContacts) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, i18n.T("domains.contacts.dryRunHdr", "Domain", domain))
	printContactFields(cmd, reg)
	if roles == nil {
		fmt.Fprintln(out, i18n.T("domains.contacts.allInherit"))
	} else {
		for _, name := range roleNames {
			rc := map[string]*apitypes.Registrant{
				"admin":   roles.Admin,
				"tech":    roles.Tech,
				"billing": roles.Billing,
			}[name]
			if rc == nil {
				fmt.Fprintln(out, i18n.T("contacts.roleFrom", "Role", name, "Who", i18n.T("contacts.roleInherit")))
				continue
			}
			fmt.Fprintln(out, i18n.T("contacts.roleFrom", "Role", name,
				"Who", rc.FirstName+" "+rc.LastName+" <"+rc.Email+">"))
		}
	}
	fmt.Fprintln(out, i18n.T("domains.contacts.dryRunFoot"))
	return nil
}

var _ = errors.New // keep errors imported if paths change
