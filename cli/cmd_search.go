package cli

import (
	"fmt"
	"strings"

	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

func newSearchCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: i18n.T("search.short"),
		Long:  i18n.T("search.long"),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			resp, err := c.Search(args[0], app.Currency)
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
}
