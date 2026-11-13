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
	var contact string
	var ownContact bool
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
			if isShow(cmd, &rf, &rolf, contact, ownContact) {
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
			switch {
			case rf.any():
				// registrant flags WIN over --own-contact, matching
				// resolveRegistrantForOrder/Transfer ordering.
				built, berr := rf.build()
				if berr != nil {
					return berr
				}
				reg = built
				var rerr error
				roles, rerr = resolveRoles(cmd, app, &rolf, reg, false, nil)
				if rerr != nil {
					return rerr
				}
			case ownContact:
				// Interactive registrant entry; needs a human on a TTY.
				if !app.p.stdinIsTTY() {
					return missingFlags("--first-name", "--last-name",
						"--email", "--phone", "--address1", "--city", "--zip", "--country")
				}
				email := ""
				if me, merr := c.Me(); merr == nil {
					email = me.Email
				}
				built := app.p.registrant(email)
				reg, prompted = &built, true
				var rerr error
				roles, rerr = resolveRoles(cmd, app, &rolf, reg, prompted, nil)
				if rerr != nil {
					return rerr
				}
			default:
				// One /v1/contact-sets fetch for the whole default branch: the
				// same response drives the default detection, the explicit
				// label resolution, and the prompt decision. A second request
				// would risk a rate-limit failure of an otherwise resolvable
				// update.
				sets, gerr := c.ContactSets()
				if gerr != nil {
					return fail(gerr)
				}
				var cs *apitypes.ContactSet
				if contact != "" {
					for i := range sets.ContactSets {
						if sets.ContactSets[i].Label == contact {
							cs = &sets.ContactSets[i]
							break
						}
					}
					if cs == nil {
						return errors.New(i18n.T("contacts.notFound", "Label", contact))
					}
				} else {
					for i := range sets.ContactSets {
						if sets.ContactSets[i].IsDefault {
							cs = &sets.ContactSets[i]
							break
						}
					}
				}
				if cs == nil {
					// No explicit label, no default: prompt on a TTY (the update
					// needs SOME registrant), otherwise name the flags.
					if app.p.stdinIsTTY() {
						email := ""
						if me, merr := c.Me(); merr == nil {
							email = me.Email
						}
						built := app.p.registrant(email)
						reg, prompted = &built, true
						var rerr error
						roles, rerr = resolveRoles(cmd, app, &rolf, reg, prompted, nil)
						if rerr != nil {
							return rerr
						}
						break
					}
					return missingFlags("--contact", "--first-name", "--last-name",
						"--email", "--phone", "--address1", "--city", "--zip", "--country")
				}
				reg = &cs.Registrant
				// nil derivation base: field specs are refused on the set path;
				// saved roles inherit unless an explicit auto/registrant spec
				// replaces the whole bundle.
				var rerr error
				roles, rerr = resolveRoles(cmd, app, &rolf, nil, false, cs.Roles)
				if rerr != nil {
					return rerr
				}
			}

			if dryRun {
				return emitDomainContactsPlan(cmd, domain, *reg, roles)
			}

			// Human-approval gate with a visible summary: contacts changes
			// can trigger ICANN verification and a registrar transfer lock
			// when the registrant changes — the human must see WHAT is
			// about to be applied, not just a bare y/N. In --json mode the
			// summary stays off stdout so the output stays parseable, and
			// without --yes an interactive run would silently wait for an
			// answer nobody can see — fail naming the flag instead.
			if jsonFlag && !yesFlag {
				return missingFlags("--yes")
			}
			if !jsonFlag {
				fmt.Fprint(cmd.OutOrStdout(), i18n.T("domains.contacts.confirmHdr", "Domain", domain))
				printContactFields(cmd, *reg)
				printRoleSummary(cmd, roles)
			}
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
	cmd.Flags().BoolVar(&ownContact, "own-contact", false, "enter the registrant contact inline")
	addRegistrantFlags(cmd, &rf)
	addRoleFlags(cmd, &rolf)
	return cmd
}

// isShow reports whether the invocation is a plain read: no registrant
// flags, no role flags, no --contact, no --own-contact.
func isShow(cmd *cobra.Command, rf *registrantFlags, rolf *roleFlags, contact string, ownContact bool) bool {
	if contact != "" || rf.any() || ownContact {
		return false
	}
	if len(rolf.parsed) == 0 {
		_ = rolf.collect(cmd)
	}
	return len(rolf.parsed) == 0
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
		// The human approves a registrar-level change; every material
		// field of an explicit role must be visible before the y/N.
		printContactFields(cmd, *rc)
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
