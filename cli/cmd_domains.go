package cli

import (
	"errors"
	"fmt"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

func newLegalCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "legal [page]",
		Short: i18n.T("legal.short"),
		Long:  i18n.T("legal.long"),
		Args:  rangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			page := "notice"
			if len(args) == 1 {
				page = args[0]
			}
			switch page {
			case "notice", "privacy", "terms", "cookies":
			default:
				return errors.New(i18n.T("legal.badPage", "Page", page))
			}
			url := app.Server + "/legal/" + page
			if openBrowser(url) {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("legal.opening", "URL", url))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("browser.openFailed", "URL", url))
			}
			return nil
		},
	}
}

func newDomainsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: i18n.T("domains.short"),
		Long:  i18n.T("domains.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.Domains()
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			if len(resp.Domains) == 0 {
				fmt.Fprintln(out, i18n.T("domains.empty"))
				return nil
			}
			rows := make([][]string, 0, len(resp.Domains))
			for _, d := range resp.Domains {
				expires := "—"
				if d.ExpireDate != nil {
					expires = d.ExpireDate.Format("2006-01-02")
				}
				rows = append(rows, []string{
					d.Name, expires, yesNo(d.Autorenew), yesNo(d.Privacy), yesNo(d.Locked),
				})
			}
			printTable(out, []string{
				i18n.T("domains.hdr.name"),
				i18n.T("domains.hdr.expires"),
				i18n.T("domains.hdr.autorenew"),
				i18n.T("domains.hdr.privacy"),
				i18n.T("domains.hdr.locked"),
			}, rows)
			return nil
		},
	}
	cmd.AddCommand(newAuthcodeCmd(app))
	cmd.AddCommand(newLockCmd(app, true))
	cmd.AddCommand(newLockCmd(app, false))
	cmd.AddCommand(newAutorenewCmd(app))
	return cmd
}

func newAuthcodeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "authcode <domain>",
		Short: i18n.T("domains.authcode.short"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			// The transfer code plus an unlocked domain is the whole
			// transfer-away path: explicit human approval required —
			// interactively the prompt, non-interactively --yes.
			fmt.Fprintln(out, i18n.T("domains.authcode.warning", "Name", args[0]))
			confirmed, err := requireYes(cmd, app)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(out, i18n.T("confirm.aborted"))
				return nil
			}
			resp, err := c.AuthCode(args[0])
			if err != nil {
				// A 409 here carries the server's "unlock first" message,
				// printed verbatim.
				return fail(err)
			}
			fmt.Fprintln(out, resp.AuthCode)
			fmt.Fprintln(out, i18n.T("domains.authcode.note"))
			return nil
		},
	}
}

func newLockCmd(app *App, locked bool) *cobra.Command {
	short := i18n.T("domains.unlock.short")
	msgKey := "domains.unlocked"
	if locked {
		short = i18n.T("domains.lock.short")
		msgKey = "domains.locked"
	}
	use := "unlock <domain>"
	if locked {
		use = "lock <domain>"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !locked {
				// Unlocking is half of the transfer-away path: explicit
				// human approval required — interactively the prompt,
				// non-interactively --yes. Locking needs no approval.
				fmt.Fprintln(out, i18n.T("domains.unlock.warning", "Name", args[0]))
				confirmed, err := requireYes(cmd, app)
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(out, i18n.T("confirm.aborted"))
					return nil
				}
			}
			if err := fail(c.SetLock(args[0], locked)); err != nil {
				return err
			}
			fmt.Fprintln(out, i18n.T(msgKey, "Name", args[0]))
			return nil
		},
	}
}

func newAutorenewCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "autorenew <domain> on|off",
		Short: i18n.T("domains.autorenew.short"),
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var enabled bool
			switch lower(args[1]) {
			case "on":
				enabled = true
			case "off":
				enabled = false
			default:
				return errors.New(i18n.T("err.invalidOnOff"))
			}
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if enabled {
				// Enabling authorises an off-session annual charge to the
				// saved payment method: explicit human approval required —
				// interactively the prompt, non-interactively --yes.
				fmt.Fprintln(out, i18n.T("domains.autorenew.warning"))
				confirmed, err := requireYes(cmd, app)
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(out, i18n.T("confirm.aborted"))
					return nil
				}
			}
			if err := fail(c.SetAutorenew(args[0], enabled)); err != nil {
				return err
			}
			msg := i18n.T("domains.autorenew.off")
			if enabled {
				msg = i18n.T("domains.autorenew.on")
			}
			fmt.Fprintln(out, msg, "Name", args[0])
			return nil
		},
	}
}

func newDNSCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: i18n.T("dns.short"),
	}
	ns := &cobra.Command{
		Use:   "ns",
		Short: i18n.T("dns.ns.short"),
	}
	ns.AddCommand(&cobra.Command{
		Use:   "get <domain>",
		Short: i18n.T("dns.nsGet.short"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.Nameservers(args[0])
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			if len(resp.Nameservers) == 0 {
				fmt.Fprintln(out, i18n.T("dns.nsEmpty"))
				return nil
			}
			fmt.Fprintln(out, i18n.T("dns.nsList", "Name", args[0]))
			for _, n := range resp.Nameservers {
				fmt.Fprintln(out, n)
			}
			return nil
		},
	})
	ns.AddCommand(&cobra.Command{
		Use:   "set <domain> <ns1> <ns2> [...]",
		Short: i18n.T("dns.nsSet.short"),
		Args:  minArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			// args[0] is the domain; the rest are the nameservers.
			if err := fail(c.SetNameservers(args[0], args[1:])); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("dns.nsSetDone"))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list <domain>",
		Short: i18n.T("dns.list.short"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.Records(args[0])
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			if len(resp.Records) == 0 {
				fmt.Fprintln(out, i18n.T("dns.empty"))
				return nil
			}
			rows := make([][]string, 0, len(resp.Records))
			for _, r := range resp.Records {
				rows = append(rows, []string{
					fmt.Sprintf("%d", r.ID), r.Host, r.Type, r.Answer, fmt.Sprintf("%d", r.TTL),
				})
			}
			printTable(out, []string{
				i18n.T("dns.hdr.id"),
				i18n.T("dns.hdr.host"),
				i18n.T("dns.hdr.type"),
				i18n.T("dns.hdr.answer"),
				i18n.T("dns.hdr.ttl"),
			}, rows)
			return nil
		},
	})
	cmd.AddCommand(newDNSSetCmd(app))
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <domain> <recordID>",
		Short: i18n.T("dns.delete.short"),
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			id, err := atoi64(args[1])
			if err != nil {
				return err
			}
			if err := fail(c.DeleteRecord(args[0], id)); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("dns.deleted"))
			return nil
		},
	})
	cmd.AddCommand(ns)
	return cmd
}

func newDNSSetCmd(app *App) *cobra.Command {
	var ttl int
	cmd := &cobra.Command{
		Use:   "set <domain> <TYPE> <host> <answer>",
		Short: i18n.T("dns.set.short"),
		Args:  exactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			rec, err := c.CreateRecord(args[0], apitypes.RecordInput{Type: args[1], Host: args[2], Answer: args[3], TTL: ttl})
			if err != nil {
				return fail(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("dns.created", "ID", rec.ID))
			return nil
		},
	}
	cmd.Flags().IntVar(&ttl, "ttl", 3600, "TTL in seconds")
	return cmd
}
