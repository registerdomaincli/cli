package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

func newContactsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contacts",
		Short: i18n.T("contacts.short"),
	}
	cmd.AddCommand(newContactsAddCmd(app))
	cmd.AddCommand(newContactsListCmd(app))
	cmd.AddCommand(newContactsShowCmd(app))
	cmd.AddCommand(newContactsDefaultCmd(app))
	cmd.AddCommand(newContactsRemoveCmd(app))
	return cmd
}

func newContactsAddCmd(app *App) *cobra.Command {
	var rf registrantFlags
	var rolf roleFlags
	cmd := &cobra.Command{
		Use:   "add <label>",
		Short: i18n.T("contacts.add.short"),
		Long:  i18n.T("contacts.add.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flag syntax fails fast, before any network call.
			if err := rolf.collect(cmd); err != nil {
				return err
			}
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			var reg apitypes.Registrant
			var prompted bool
			switch {
			case rf.any():
				built, err := rf.build()
				if err != nil {
					return err
				}
				reg = *built
			case app.p.stdinIsTTY():
				email := ""
				if me, err := c.Me(); err == nil {
					email = me.Email
				}
				reg = app.p.registrant(email)
				prompted = true
			default:
				return missingFlags("--first-name", "--last-name", "--email", "--phone",
					"--address1", "--city", "--zip", "--country")
			}
			roles, err := resolveRoles(cmd, app, &rolf, &reg, prompted, nil)
			if err != nil {
				return fail(err)
			}
			if _, err := c.CreateContactSet(args[0], reg, roles); err != nil {
				return fail(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.saved", "Label", args[0]))
			return nil
		},
	}
	addRegistrantFlags(cmd, &rf)
	addRoleFlags(cmd, &rolf)
	return cmd
}

func newContactsListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: i18n.T("contacts.list.short"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.ContactSets()
			if err != nil {
				return fail(err)
			}
			if len(resp.ContactSets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.empty"))
				return nil
			}
			for _, s := range resp.ContactSets {
				def := ""
				if s.IsDefault {
					def = " *"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s\t%s %s\t%s\n",
					s.Label, def, s.Registrant.FirstName, s.Registrant.LastName, s.Registrant.Email)
			}
			return nil
		},
	}
}

// findContactSet locates a saved set by label.
func findContactSet(c *Client, label string) (*apitypes.ContactSet, error) {
	resp, err := c.ContactSets()
	if err != nil {
		return nil, fail(err)
	}
	for i := range resp.ContactSets {
		if resp.ContactSets[i].Label == label {
			return &resp.ContactSets[i], nil
		}
	}
	return nil, errors.New(i18n.T("contacts.notFound", "Label", label))
}

func newContactsShowCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <label>",
		Short: i18n.T("contacts.show.short"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			s, err := findContactSet(c, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			r := s.Registrant
			fmt.Fprintf(out, "label:       %s\n", s.Label)
			fmt.Fprintf(out, "default:     %s\n", yesNo(s.IsDefault))
			fmt.Fprintf(out, "firstName:   %s\n", r.FirstName)
			fmt.Fprintf(out, "lastName:    %s\n", r.LastName)
			fmt.Fprintf(out, "organization:%s\n", withSpace(r.Organization))
			fmt.Fprintf(out, "email:       %s\n", r.Email)
			fmt.Fprintf(out, "phone:       %s\n", r.Phone)
			fmt.Fprintf(out, "address1:    %s\n", r.Address1)
			fmt.Fprintf(out, "address2:    %s\n", r.Address2)
			fmt.Fprintf(out, "city:        %s\n", r.City)
			fmt.Fprintf(out, "state:       %s\n", r.State)
			fmt.Fprintf(out, "zip:         %s\n", r.Zip)
			fmt.Fprintf(out, "country:     %s\n", r.Country)
			if s.Roles != nil {
				for _, name := range roleNames {
					c := map[string]*apitypes.Registrant{
						"admin":   s.Roles.Admin,
						"tech":    s.Roles.Tech,
						"billing": s.Roles.Billing,
					}[name]
					if c == nil {
						fmt.Fprintln(out, i18n.T("contacts.roleFrom", "Role", name, "Who", i18n.T("contacts.roleInherit")))
						continue
					}
					fmt.Fprintln(out, i18n.T("contacts.roleFrom", "Role", name,
						"Who", c.FirstName+" "+c.LastName+" <"+c.Email+">"))
				}
			}
			return nil
		},
	}
}

func withSpace(s string) string {
	if s == "" {
		return s
	}
	return " " + s
}

func newContactsDefaultCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "default <label>",
		Short: i18n.T("contacts.default.short"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			s, err := findContactSet(c, args[0])
			if err != nil {
				return err
			}
			if _, err := c.SetDefaultContactSet(s.ID); err != nil {
				return fail(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.defaultSet", "Label", s.Label))
			return nil
		},
	}
}

func newContactsRemoveCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <label>",
		Short: i18n.T("contacts.remove.short"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			s, err := findContactSet(c, args[0])
			if err != nil {
				return err
			}
			if err := fail(c.DeleteContactSet(s.ID)); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.removed", "Label", s.Label))
			return nil
		},
	}
}

// billingFlags carries the parametric billing profile fields.
type billingFlags struct {
	LegalName, Company, TaxID, Email, Address1, Address2, City, State, Zip, Country string
}

func (bf *billingFlags) any() bool {
	return bf.LegalName != "" || bf.Company != "" || bf.TaxID != "" || bf.Email != "" ||
		bf.Address1 != "" || bf.Address2 != "" || bf.City != "" || bf.State != "" ||
		bf.Zip != "" || bf.Country != ""
}

func registerBillingFlags(cmd *cobra.Command, bf *billingFlags) {
	cmd.Flags().StringVar(&bf.LegalName, "legal-name", "", "billing legal name")
	cmd.Flags().StringVar(&bf.Company, "company", "", "company (optional)")
	cmd.Flags().StringVar(&bf.TaxID, "tax-id", "", "VAT/tax id (optional)")
	cmd.Flags().StringVar(&bf.Email, "billing-email", "", "billing email")
	cmd.Flags().StringVar(&bf.Address1, "address1", "", "billing address line 1")
	cmd.Flags().StringVar(&bf.Address2, "address2", "", "billing address line 2 (optional)")
	cmd.Flags().StringVar(&bf.City, "city", "", "billing city")
	cmd.Flags().StringVar(&bf.State, "state", "", "billing state (optional)")
	cmd.Flags().StringVar(&bf.Zip, "zip", "", "billing postal code")
	cmd.Flags().StringVar(&bf.Country, "country", "", "billing country (ISO 3166-1 alpha-2)")
}

func newBillingCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: i18n.T("billing.short"),
	}
	cmd.AddCommand(newBillingShowCmd(app))
	cmd.AddCommand(newBillingSetCmd(app))
	cmd.AddCommand(newBillingPortalCmd(app))
	return cmd
}

func newBillingShowCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: i18n.T("billing.show.short"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			p, err := c.Billing()
			if err != nil {
				return fail(err)
			}
			if p.LegalName == "" && p.Email == "" {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("billing.empty"))
				return nil
			}
			if err := emitJSON(cmd, p); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "legalName: %s\n", p.LegalName)
			fmt.Fprintf(out, "company:   %s\n", p.Company)
			fmt.Fprintf(out, "taxID:     %s\n", p.TaxID)
			fmt.Fprintf(out, "email:     %s\n", p.Email)
			fmt.Fprintf(out, "address1:  %s\n", p.Address1)
			fmt.Fprintf(out, "address2:  %s\n", p.Address2)
			fmt.Fprintf(out, "city:      %s\n", p.City)
			fmt.Fprintf(out, "state:     %s\n", p.State)
			fmt.Fprintf(out, "zip:       %s\n", p.Zip)
			fmt.Fprintf(out, "country:   %s\n", p.Country)
			return nil
		},
	}
}

func newBillingSetCmd(app *App) *cobra.Command {
	var bf billingFlags
	cmd := &cobra.Command{
		Use:   "set",
		Short: i18n.T("billing.set.short"),
		Long:  i18n.T("billing.set.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			var p apitypes.BillingProfile
			switch {
			case bf.any():
				var missing []string
				for f, v := range map[string]string{
					"--legal-name": bf.LegalName, "--billing-email": bf.Email,
					"--address1": bf.Address1, "--city": bf.City,
					"--zip": bf.Zip, "--country": bf.Country,
				} {
					if v == "" {
						missing = append(missing, f)
					}
				}
				if len(missing) > 0 {
					return missingFlags(missing...)
				}
				p = apitypes.BillingProfile{
					LegalName: bf.LegalName, Company: bf.Company, TaxID: bf.TaxID,
					Email: bf.Email, Address1: bf.Address1, Address2: bf.Address2,
					City: bf.City, State: bf.State, Zip: bf.Zip,
					Country: strings.ToUpper(bf.Country),
				}
			case app.p.stdinIsTTY():
				pr := app.p
				p.LegalName = pr.required(i18n.T("billing.prompt.legalName"))
				p.Company = pr.ask(i18n.T("billing.prompt.company"))
				p.TaxID = pr.ask(i18n.T("billing.prompt.taxID"))
				p.Email = pr.required(i18n.T("billing.prompt.email"))
				p.Address1 = pr.required(i18n.T("billing.prompt.address1"))
				p.Address2 = pr.ask(i18n.T("billing.prompt.address2"))
				p.City = pr.required(i18n.T("billing.prompt.city"))
				p.State = pr.ask(i18n.T("billing.prompt.state"))
				p.Zip = pr.required(i18n.T("billing.prompt.zip"))
				p.Country = strings.ToUpper(pr.required(i18n.T("billing.prompt.country")))
			default:
				return missingFlags("--legal-name", "--billing-email", "--address1", "--city", "--zip", "--country")
			}
			if _, err := c.UpdateBilling(p); err != nil {
				return fail(err)
			}
			if err := emitJSON(cmd, p); err != nil {
				return err
			}
			fmt.Fprintln(out, i18n.T("billing.saved"))
			return nil
		},
	}
	cmd.Flags().StringVar(&bf.LegalName, "legal-name", "", "billing legal name")
	cmd.Flags().StringVar(&bf.Company, "company", "", "company (optional)")
	cmd.Flags().StringVar(&bf.TaxID, "tax-id", "", "VAT/tax id (optional)")
	cmd.Flags().StringVar(&bf.Email, "billing-email", "", "billing email")
	cmd.Flags().StringVar(&bf.Address1, "address1", "", "billing address line 1")
	cmd.Flags().StringVar(&bf.Address2, "address2", "", "billing address line 2 (optional)")
	cmd.Flags().StringVar(&bf.City, "city", "", "billing city")
	cmd.Flags().StringVar(&bf.State, "state", "", "billing state (optional)")
	cmd.Flags().StringVar(&bf.Zip, "zip", "", "billing postal code")
	cmd.Flags().StringVar(&bf.Country, "country", "", "billing country (ISO 3166-1 alpha-2)")
	return cmd
}

func newBillingPortalCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "portal",
		Short: i18n.T("billing.portal.short"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.Portal()
			if err != nil {
				return fail(err)
			}
			if openBrowser(resp.URL) {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("billing.portalOpening", "URL", resp.URL))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("browser.openFailed", "URL", resp.URL))
			}
			return nil
		},
	}
}

// newPaymentMethodsCmd lists the saved payment methods behind the
// off-session fast checkout. Agents check it before placing an order so
// they can ask the human which rail to pay on; removing a method happens in
// the web panel's payment page (the server exposes DELETE, but the CLI
// stays read-only on revoking consent).
func newPaymentMethodsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "payment-methods",
		Short: i18n.T("paymentmethods.short"),
		Long:  i18n.T("paymentmethods.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.PaymentMethods()
			if err != nil {
				return fail(err)
			}
			if jsonFlag {
				return emitJSON(cmd, resp)
			}
			out := cmd.OutOrStdout()
			if len(resp.Methods) == 0 {
				fmt.Fprintln(out, i18n.T("paymentmethods.empty"))
				return nil
			}
			rows := make([][]string, 0, len(resp.Methods))
			for _, m := range resp.Methods {
				rows = append(rows, []string{m.ID, m.Method, m.Details, m.Status})
			}
			printTable(out, []string{
				i18n.T("paymentmethods.hdr.id"),
				i18n.T("paymentmethods.hdr.method"),
				i18n.T("paymentmethods.hdr.details"),
				i18n.T("paymentmethods.hdr.status"),
			}, rows)
			return nil
		},
	}
}
