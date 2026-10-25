package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// Registrant resolution on the CLI mirrors the server's precedence, with a
// parametric-first contract:
//
//  1. registrant flags (--first-name … --country) — fully non-interactive
//  2. --contact <label>                           — a saved contact set
//  3. the user's default contact set (server-side)
//  4. interactive prompts (only when stdin is a TTY)
//  5. otherwise: error naming the flags to pass

func newRegisterCmd(app *App) *cobra.Command {
	var years int
	var ownContact bool
	var contact, saveContact string
	var dryRun, noWait bool
	var rf registrantFlags
	cmd := &cobra.Command{
		Use:   "register <domain>",
		Short: i18n.T("register.short"),
		Long:  i18n.T("register.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if years < 1 || years > 10 {
				return errors.New(i18n.T("err.invalidYears"))
			}
			var reg *apitypes.Registrant
			var setID string
			var prompted bool
			if !dryRun {
				var err error
				reg, setID, prompted, err = resolveRegistrantForOrder(cmd, app, ownContact, contact, &rf)
				if err != nil {
					return err
				}
			}
			req := apitypes.CreateOrderRequest{
				Kind:         "registration",
				Domain:       args[0],
				Years:        years,
				Registrant:   reg,
				ContactSetID: setID,
			}
			created, err := orderFlow(cmd, app, req, dryRun, noWait)
			if err != nil {
				return err
			}
			if created && reg != nil {
				if saveContact != "" {
					// Explicit parameter beats the interactive offer.
					if _, err := app.Client.CreateContactSet(saveContact, *reg); err != nil {
						fmt.Fprintln(cmd.ErrOrStderr(), fail(err))
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), i18n.T("contacts.saved", "Label", saveContact))
					}
				} else {
					maybeSaveContactSet(cmd, app, reg, prompted)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&years, "years", "y", 1, "years to register (1-10)")
	cmd.Flags().BoolVar(&ownContact, "own-contact", false, "enter the registrant contact inline")
	cmd.Flags().StringVar(&contact, "contact", "", "use the saved contact set with this label")
	cmd.Flags().StringVar(&saveContact, "save-contact", "", "save the inline registrant as a contact set with this label (no prompt)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the price and stop before creating the order")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "print the order id + checkout URL and exit (complete payment asynchronously)")
	addRegistrantFlags(cmd, &rf)
	return cmd
}

func newRenewCmd(app *App) *cobra.Command {
	var years int
	var dryRun, noWait bool
	cmd := &cobra.Command{
		Use:   "renew <domain>",
		Short: i18n.T("renew.short"),
		Long:  i18n.T("renew.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if years < 1 || years > 10 {
				return errors.New(i18n.T("err.invalidYears"))
			}
			req := apitypes.CreateOrderRequest{Kind: "renewal", Domain: args[0], Years: years}
			_, err := orderFlow(cmd, app, req, dryRun, noWait)
			return err
		},
	}
	cmd.Flags().IntVarP(&years, "years", "y", 1, "years to renew (1-10)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the price and stop before creating the order")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "print the order id + checkout URL and exit")
	return cmd
}

func newTransferCmd(app *App) *cobra.Command {
	var contact string
	var authCode string
	var rf registrantFlags
	var dryRun, noWait bool
	cmd := &cobra.Command{
		Use:   "transfer <domain> --auth-code <code>",
		Short: i18n.T("transfer.short"),
		Long:  i18n.T("transfer.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(authCode) == "" {
				return missingFlags("--auth-code")
			}
			reg, setID, prompted, err := resolveRegistrantForTransfer(cmd, app, contact, &rf)
			if err != nil {
				return err
			}
			req := apitypes.CreateOrderRequest{
				Kind:         "transfer",
				Domain:       args[0],
				AuthCode:     authCode,
				Registrant:   reg,
				ContactSetID: setID,
			}
			created, err := orderFlow(cmd, app, req, dryRun, noWait)
			if err != nil {
				return err
			}
			if created {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("transfer.explainer"))
				maybeSaveContactSet(cmd, app, reg, prompted)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&authCode, "auth-code", "", "EPP/auth code from the losing registrar (required)")
	cmd.Flags().StringVar(&contact, "contact", "", "use the saved contact set with this label")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the price and stop before creating the order")
	addRegistrantFlags(cmd, &rf)
	return cmd
}

func newOrdersCmd(app *App) *cobra.Command {
	var cancel string
	cmd := &cobra.Command{
		Use:   "orders",
		Short: i18n.T("orders.short"),
		Long:  i18n.T("orders.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if cancel != "" {
				if _, err := c.CancelOrder(cancel); err != nil {
					return fail(err)
				}
				fmt.Fprintln(out, i18n.T("orders.cancelled", "ID", cancel))
				return nil
			}
			resp, err := c.Orders()
			if err != nil {
				return fail(err)
			}
			if jsonFlag {
				return emitJSON(cmd, resp)
			}
			if len(resp.Orders) == 0 {
				fmt.Fprintln(out, i18n.T("orders.empty"))
				return nil
			}
			rows := make([][]string, 0, len(resp.Orders))
			for _, o := range resp.Orders {
				rows = append(rows, []string{
					o.ID, o.Kind, o.Domain, o.Status, o.InvoiceNumber, o.CheckoutURL,
				})
			}
			printTable(out, []string{
				i18n.T("orders.hdr.id"),
				i18n.T("orders.hdr.kind"),
				i18n.T("orders.hdr.domain"),
				i18n.T("orders.hdr.status"),
				i18n.T("orders.hdr.invoice"),
				i18n.T("orders.hdr.checkout"),
			}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&cancel, "cancel", "", "cancel the order with this id")
	return cmd
}

func newInvoiceCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "invoice <orderId>",
		Short: i18n.T("invoice.short"),
		Long:  i18n.T("invoice.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			ord, err := c.Order(args[0])
			if err != nil {
				return fail(err)
			}
			if ord.InvoiceURL == "" {
				return errors.New(i18n.T("invoice.noInvoice"))
			}
			// Always the pre-signed URL from the order — a browser carries
			// no Authorization header, so the bare /v1/invoices path 401s.
			if openBrowser(ord.InvoiceURL) {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("invoice.opening", "URL", ord.InvoiceURL))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("browser.openFailed", "URL", ord.InvoiceURL))
			}
			return nil
		},
	}
}
