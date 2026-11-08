package cli

import (
	"fmt"
	"strings"

	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

func newSearchCmd(app *App) *cobra.Command {
	var tldsFlag []string
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: i18n.T("search.short"),
		Long:  i18n.T("search.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.Search(args[0], app.Currency, tldsFlag, limitFlag)
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			if len(resp.Results) == 0 {
				fmt.Fprintln(out, i18n.T("search.empty"))
				return nil
			}
			// Every row carries the currency the server priced in; the
			// header shows it once so the agent never has to guess.
			cur := ""
			for _, r := range resp.Results {
				if r.Currency != "" {
					cur = strings.ToUpper(r.Currency)
					break
				}
			}
			rows := make([][]string, 0, len(resp.Results))
			for _, r := range resp.Results {
				purchase, renewal := "—", "—"
				if r.Purchasable {
					if r.PurchasePrice != nil {
						purchase = formatMoney(dollarsToCents(*r.PurchasePrice), r.Currency)
					}
					if r.RenewalPrice != nil {
						renewal = formatMoney(dollarsToCents(*r.RenewalPrice), r.Currency)
					}
				}
				rows = append(rows, []string{
					r.Name,
					yesNo(r.Purchasable),
					purchase,
					renewal,
					yesNo(r.Premium),
				})
			}
			printTable(out, []string{
				i18n.T("search.hdr.name"),
				i18n.T("search.hdr.available"),
				i18n.T("search.hdr.priceYr", "Currency", cur),
				i18n.T("search.hdr.renewal", "Currency", cur),
				i18n.T("search.hdr.premium"),
			}, rows)
			// The ~ prefix on the price columns and this footer both flag
			// the prices as indicative; billing uses GET /v1/quote only.
			fmt.Fprintln(out, i18n.T("search.footer"))
			return nil
		},
	}
	// Help text is one of the five deliberate literal-string exceptions
	// (repo rule: flag help stays in English for agent parseability).
	cmd.Flags().StringSliceVar(&tldsFlag, "tlds", nil, "restrict results to these TLDs, comma-separated (e.g. com,net,org; run 'tlds' to list the catalog)")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum number of rows to return (server default 50, ceiling 50)")
	return cmd
}

func newTLDsCmd(app *App) *cobra.Command {
	var groupFlag string
	cmd := &cobra.Command{
		Use:   "tlds",
		Short: i18n.T("tlds.short"),
		Long:  i18n.T("tlds.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			cat, err := c.TLDs(groupFlag)
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			if jsonFlag {
				return emitJSON(cmd, cat)
			}
			fmt.Fprintln(out, i18n.T("tlds.groupsHdr"))
			for _, g := range cat.Groups {
				fmt.Fprintf(out, "  %-12s %s — %s\n", g.Slug, g.Label, g.Description)
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, i18n.T("tlds.listHdr", "Count", len(cat.TLDs)))
			names := make([]string, 0, len(cat.TLDs))
			for _, e := range cat.TLDs {
				names = append(names, e.TLD)
			}
			// Wrapped columns, not a table: agents grep the flat list.
			printWrapped(out, names, 8)
			if groupFlag != "" {
				fmt.Fprintln(out, i18n.T("tlds.filteredFooter", "Group", groupFlag))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&groupFlag, "group", "", "only list TLDs belonging to this group slug (see the groups section of plain output)")
	return cmd
}
