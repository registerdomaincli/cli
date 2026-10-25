package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// Poll cadence, indirected so tests run the same code instantly.
var (
	pollInterval = 3 * time.Second
	pollTimeout  = 15 * time.Minute
)

// orderFlow runs the shared quote → confirm → order → checkout → poll
// sequence behind register/renew/transfer. dryRun prints the confirm line and
// stops without creating an order. The returned bool reports whether an order
// was actually created.
//
// Non-interactive (agents): --yes approves placement, --no-wait returns right
// after the order is created (the checkout URL is printed for the human), and
// without --yes on a non-TTY stdin the command fails naming the flag — the
// human-approval gate never silently passes.
func orderFlow(cmd *cobra.Command, app *App, req apitypes.CreateOrderRequest, dryRun bool, noWait bool) (bool, error) {
	out := cmd.OutOrStdout()
	// The requested currency rides on both the quote and the order so the
	// server prices, records and charges in one currency.
	req.Currency = app.Currency
	quote, err := app.Client.Quote(req.Domain, req.Years, req.Kind, app.Currency)
	if err != nil {
		return false, fail(err)
	}
	if !jsonFlag {
		fmt.Fprintf(out, "%s", i18n.T("order.confirm",
			"Domain", quote.Domain,
			"Years", quote.Years,
			"Amount", formatMoney(quote.NetCents, quote.Currency)))
	}
	if dryRun {
		if !jsonFlag {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, i18n.T("order.dryRun"))
		return false, nil
	}
	confirmed, err := requireYes(cmd, app)
	if err != nil {
		return false, err
	}
	if !jsonFlag {
		fmt.Fprintln(out)
	}
	if !confirmed {
		fmt.Fprintln(out, i18n.T("confirm.aborted"))
		return false, nil
	}
	ord, err := app.Client.CreateOrder(req)
	if err != nil {
		return false, fail(err)
	}
	if !jsonFlag {
		fmt.Fprintln(out, i18n.T("order.created", "ID", ord.ID))
		if stdoutIsTTY() && openBrowser(ord.CheckoutURL) {
			fmt.Fprintln(out, i18n.T("order.checkoutOpen"))
		} else {
			fmt.Fprintln(out, i18n.T("order.checkoutUrl", "URL", ord.CheckoutURL))
		}
	}
	if err := emitJSON(cmd, ord); err != nil {
		return true, err
	}
	if noWait {
		// Agents: the order id + checkout URL are in the JSON output; the
		// order can always be reopened via `domain-cli orders`.
		return true, nil
	}
	if err := pollOrder(cmd, app.Client, ord.ID); err != nil {
		return true, err
	}
	return true, nil
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// pollOrder watches GET /v1/orders/{id} every 3s for up to 15 minutes,
// printing status transitions and the assigned invoice number. Terminal
// failure states print the failure reason and refund status; a timeout
// points at `domain-cli orders`.
func pollOrder(cmd *cobra.Command, c *Client, id string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, i18n.T("order.polling"))
	deadline := time.Now().Add(pollTimeout)
	last := ""
	for {
		ord, err := c.Order(id)
		if err != nil {
			return fail(err)
		}
		if ord.Status != last {
			fmt.Fprintln(out, i18n.T("order.statusLine", "Status", ord.Status))
			if ord.InvoiceNumber != "" {
				fmt.Fprintln(out, i18n.T("order.invoiceLine", "Number", ord.InvoiceNumber))
			}
			last = ord.Status
		}
		switch ord.Status {
		case "completed":
			return nil
		case "failed":
			fmt.Fprintln(out, i18n.T("order.failed", "Reason", ord.FailureReason))
			return nil
		case "refunded":
			fmt.Fprintln(out, i18n.T("order.failed", "Reason", ord.FailureReason))
			fmt.Fprintln(out, i18n.T("order.refunded"))
			return nil
		case "expired":
			fmt.Fprintln(out, i18n.T("order.expired"))
			return nil
		case "cancelled":
			fmt.Fprintln(out, i18n.T("order.cancelledLine"))
			return nil
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(out, i18n.T("order.timeout", "ID", id))
			return nil
		}
		time.Sleep(pollInterval)
	}
}

// resolveRegistrant implements the CLI side of the registrant precedence:
// inline prompt (--own-contact) > contact set by label > the account's
// default set. When no default set exists the user is prompted; the caller
// offers to save the answers as a new contact set. The bool reports whether
// an inline prompt happened.
func resolveRegistrant(cmd *cobra.Command, app *App, ownContact bool, contactLabel string) (*apitypes.Registrant, string, bool, error) {
	c, err := app.requireClient()
	if err != nil {
		return nil, "", false, err
	}
	switch {
	case ownContact:
		email := ""
		if me, err := c.Me(); err == nil {
			email = me.Email
		}
		reg := app.p.registrant(email)
		return &reg, "", true, nil
	case contactLabel != "":
		sets, err := c.ContactSets()
		if err != nil {
			return nil, "", false, fail(err)
		}
		for _, s := range sets.ContactSets {
			if s.Label == contactLabel {
				return nil, s.ID, false, nil
			}
		}
		return nil, "", false, errors.New(i18n.T("contacts.notFound", "Label", contactLabel))
	default:
		sets, err := c.ContactSets()
		if err != nil {
			return nil, "", false, fail(err)
		}
		for _, s := range sets.ContactSets {
			if s.IsDefault {
				return nil, "", false, nil
			}
		}
		// Contacts are mandatory for registration and transfer; with no
		// default set the server would reject the order. Prompt only when a
		// human is attached; agents pass the registrant flags or --contact.
		if !app.p.stdinIsTTY() {
			return nil, "", false, missingFlags("--contact", "--first-name", "--last-name",
				"--email", "--phone", "--address1", "--city", "--zip", "--country")
		}
		email := ""
		if me, err := c.Me(); err == nil {
			email = me.Email
		}
		reg := app.p.registrant(email)
		return &reg, "", true, nil
	}
}

// resolveRegistrantForOrder resolves the registrant for registration:
// flags > --contact label > default set > interactive prompt (TTY) > error.
func resolveRegistrantForOrder(cmd *cobra.Command, app *App, ownContact bool, contactLabel string, rf *registrantFlags) (*apitypes.Registrant, string, bool, error) {
	if rf.any() {
		reg, err := rf.build()
		return reg, "", false, err
	}
	return resolveRegistrant(cmd, app, ownContact, contactLabel)
}

// resolveRegistrantForTransfer is the transfer variant: contacts are
// mandatory there too, and without registrant flags on a non-TTY stdin the
// command fails naming them (never a silent default that would make someone
// else the registrant).
func resolveRegistrantForTransfer(cmd *cobra.Command, app *App, contactLabel string, rf *registrantFlags) (*apitypes.Registrant, string, bool, error) {
	if rf.any() {
		reg, err := rf.build()
		return reg, "", false, err
	}
	return resolveRegistrant(cmd, app, false, contactLabel)
}

// dryRunFlag / noWaitFlag read the order-flow flags off any command.
func dryRunFlag(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("dry-run")
	return v
}

func noWaitFlag(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool("no-wait")
	if err != nil {
		return false
	}
	return v
}

// resolveRegistrant implements the CLI side of the registrant precedence:
func maybeSaveContactSet(cmd *cobra.Command, app *App, reg *apitypes.Registrant, prompted bool) {
	if !prompted || reg == nil {
		return
	}
	out := cmd.OutOrStdout()
	if !app.p.confirm(i18n.T("contacts.savePrompt")) {
		return
	}
	label := app.p.required(i18n.T("contacts.labelPrompt"))
	if _, err := app.Client.CreateContactSet(label, *reg); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), fail(err))
		return
	}
	fmt.Fprintln(out, i18n.T("contacts.saved", "Label", label))
}
