package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// App carries the resolved configuration and client to every command. It is
// populated once per execution in the root's PersistentPreRunE.
type App struct {
	Server string
	Token  string
	Client *Client
	// Currency is the preferred billing currency (ISO 4217, lowercase)
	// resolved from --currency / DOMAIN_CLI_CURRENCY / config. Empty means
	// the server picks its default.
	Currency string
	// tokenFromConfig reports that Token was read from config.json rather
	// than named by --token/--token-file/DOMAIN_CLI_TOKEN. A command that ends
	// a session only touches the file when the file supplied the credential.
	// It is derived from which flags the caller set, not from the resolved
	// value: an empty --token-file resolves to the config token without the
	// caller ever naming it.
	tokenFromConfig bool
	// p reads user answers for the whole run; one shared buffered reader so
	// piped input is not lost between prompts.
	p *prompter
}

// requireClient returns the client when the user is signed in; every command
// that talks to an authenticated endpoint funnels through here.
func (a *App) requireClient() (*Client, error) {
	if a.Client == nil || a.Token == "" {
		return nil, errors.New(i18n.T("err.notLoggedIn"))
	}
	return a.Client, nil
}

// fail converts transport errors into localized ones. APIError messages from
// the server are printed verbatim by design (plan step 9): that is how
// "not managed by us" surfaces. Rate limits get the localized message.
func fail(err error) error {
	var rl *RateLimitedError
	if errors.As(err, &rl) {
		return errors.New(i18n.T("err.ratelimited", "Seconds", rl.Seconds))
	}
	return err
}

// resolveTokenFlag returns the token given on the command line, reading
// --token-file when --token is absent. A file keeps the credential out of the
// process table and shell history: an argv token is world-readable on Linux
// via /proc/<pid>/cmdline and lands in ~/.bash_history.
func resolveTokenFlag() (string, error) {
	if tokenFlag != "" || tokenFileFlag == "" {
		return tokenFlag, nil
	}
	data, err := os.ReadFile(tokenFileFlag)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", tokenFileFlag, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// warnLaxConfigPerms warns when an existing config file is readable by other
// users. Save re-asserts the mode on the next write, but a file that predates
// this build, or was restored from a tarball, keeps the token exposed until
// then. Dormant accounts count: `accounts` carries tokens too, so a config
// whose live session was signed out is still a credential file.
func warnLaxConfigPerms(cmd *cobra.Command, cfg Config) {
	if !cfg.carriesToken() {
		return
	}
	path, err := ConfigPath()
	if err != nil {
		return
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm()&0o077 == 0 {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("warn.configPerms",
		"Path", path, "Mode", fmt.Sprintf("%04o", fi.Mode().Perm())))
}

// annotationNoRootToken marks a command that never sends the root client's
// credential: the auth endpoints are public (signup/login), `account list`
// touches no endpoint, and `account switch`/`account remove` build their own
// client from the selected entry. Two things follow: the server-binding guard —
// which exists to keep app.Token from being sent to a host named by --server —
// does not apply, and the root client is built without the token.
const annotationNoRootToken = "domain-cli/no-root-token"

// noRootToken reports whether the command was marked as never sending the root
// client's credential.
func noRootToken(cmd *cobra.Command) bool {
	return cmd.Annotations[annotationNoRootToken] == "true"
}

// annotationLocal marks a command that does not need the root server resolved:
// `account list` only reads the config, `account switch`/`account remove` use
// the selected entry's own server, and a `logout` with no session has nothing
// to revoke. Without it the root demands --server from a machine that has never
// named one.
const annotationLocal = "domain-cli/local"

// localOnly reports whether the command was marked as not needing the root
// server resolved.
func localOnly(cmd *cobra.Command) bool {
	return cmd.Annotations[annotationLocal] == "true"
}

// NewRootCmd builds the whole command tree. i18n.Detect() has already run in
// the i18n package init, so the localized Short/Long strings captured here
// reflect the environment locale.
func NewRootCmd(version string) *cobra.Command {
	app := &App{}

	root := &cobra.Command{
		Use:          "domain-cli",
		Short:        i18n.T("root.short"),
		Long:         i18n.T("root.long"),
		Version:      version,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := Load()
			readable := err == nil
			if !readable {
				cfg = Config{}
			}
			warnLaxConfigPerms(cmd, cfg)
			explicitToken, err := resolveTokenFlag()
			if err != nil {
				return err
			}
			app.Server = ResolveServer(serverFlag, cfg)
			app.Token = ResolveToken(explicitToken, cfg)
			app.tokenFromConfig = !cmd.Flags().Changed("token") &&
				!cmd.Flags().Changed("token-file") &&
				os.Getenv("DOMAIN_CLI_TOKEN") == "" && cfg.Token != ""
			// Prompts go to stderr: they are a conversation with the human, not
			// program output. On stdout they would precede the JSON document
			// under --json and make it unparseable.
			app.p = newPrompter(cmd.InOrStdin(), cmd.ErrOrStderr())
			if app.Server == "" {
				// An endpoint is required unless the command can finish
				// without one: `account list` only reads the config, and a
				// `logout` with no session has nothing to revoke. A command
				// that would send the root credential still needs a host.
				if !localOnly(cmd) || (app.Token != "" && !noRootToken(cmd)) {
					if app.p.stdinIsTTY() {
						def := DefaultServerURL()
						// stderr, like every other prompt: on stdout this prose
						// would precede the JSON document under --json.
						fmt.Fprintf(cmd.ErrOrStderr(), "%s", i18n.T("config.serverPrompt", "Default", def))
						if entered := app.p.line(); entered != "" {
							app.Server = entered
						} else {
							app.Server = def
						}
						if readable {
							// Never write back a config that failed to parse:
							// saving the zero value would drop every saved
							// sign-in. The resolved server still applies.
							cfg.ServerURL = app.Server
							_ = Save(cfg)
						}
					} else {
						// Non-interactive: never guess a server. Agents pass
						// --server or DOMAIN_CLI_SERVER_URL.
						return missingFlags("--server")
					}
				}
			}
			// A command that touches no endpoint at all must not be blocked by
			// the shape of a server it will never call: `account list` reads
			// the config, and `account switch`/`account remove` use the
			// selected entry's own server. `signup`/`login` are no-root-token
			// but do call the auth endpoints, so they are still validated.
			if app.Server != "" && !(localOnly(cmd) && noRootToken(cmd)) {
				if err := ValidateServerURL(app.Server); err != nil {
					return err
				}
				// The binding guard keeps a stored token from being sent to a
				// host the caller just named. It only protects commands that
				// actually send that token.
				if !noRootToken(cmd) && !tokenBelongsToServer(explicitToken, cfg, app.Server) {
					return errors.New(i18n.T("err.tokenServerMismatch",
						"StoredServer", cfg.ServerURL, "Server", app.Server))
				}
			}
			clientToken := app.Token
			if noRootToken(cmd) {
				clientToken = ""
			}
			app.Client = NewClient(app.Server, clientToken, i18n.Current())
			app.Currency = ResolveCurrency(currencyFlag, cfg)
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			notifyUpdate(cmd, version)
			return nil
		},
	}
	root.PersistentFlags().StringVar(&serverFlag, "server", "", "RegisterDomainCLI server URL (overrides config and env)")
	root.PersistentFlags().StringVar(&currencyFlag, "currency", "", "billing currency: ISO 4217 code the server supports, e.g. eur, usd (overrides config and env)")
	root.PersistentFlags().StringVar(&tokenFlag, "token", "", "API token (overrides config and env)")
	root.PersistentFlags().StringVar(&tokenFileFlag, "token-file", "", "read the API token from a file (used when --token is absent)")
	root.PersistentFlags().BoolVar(&jsonFlag, "json", false, "machine-readable JSON output (for agents)")
	root.PersistentFlags().BoolVar(&yesFlag, "yes", false, "assume yes for every confirmation (non-interactive use)")
	root.SetVersionTemplate("domain-cli version {{.Version}}\n")
	root.SetErrPrefix(i18n.T("err.prefix"))
	root.SetUsageTemplate(localizedUsageTemplate())
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: i18n.T("tmpl.helpShort"),
		Run: func(c *cobra.Command, args []string) {
			target, _, err := c.Root().Find(args)
			if target == nil || err != nil {
				c.Printf("Unknown help topic %#q\n", args)
				_ = c.Root().Usage()
			} else {
				target.InitDefaultHelpFlag()
				_ = target.Help()
			}
		},
	})

	root.AddCommand(newSignupCmd(app))
	root.AddCommand(newLoginCmd(app))
	root.AddCommand(newLogoutCmd(app))
	root.AddCommand(newWhoamiCmd(app))
	root.AddCommand(newAccountCmd(app))
	root.AddCommand(newSearchCmd(app))
	root.AddCommand(newRegisterCmd(app))
	root.AddCommand(newRenewCmd(app))
	root.AddCommand(newTransferCmd(app))
	root.AddCommand(newOrdersCmd(app))
	root.AddCommand(newInvoiceCmd(app))
	root.AddCommand(newContactsCmd(app))
	root.AddCommand(newBillingCmd(app))
	root.AddCommand(newLegalCmd(app))
	root.AddCommand(newDomainsCmd(app))
	root.AddCommand(newDNSCmd(app))
	return root
}

var (
	serverFlag    string
	tokenFlag     string
	tokenFileFlag string
	currencyFlag  string
)

// localizedUsageTemplate overrides cobra's default usage template, whose
// scaffolding words ("Usage:", "Flags:", "Available Commands:", "Global
// Flags:") are fixed English that Short/Long values cannot translate.
func localizedUsageTemplate() string {
	return strings.NewReplacer(
		"@@USAGE@@", i18n.T("tmpl.usage"),
		"@@ALIASES@@", i18n.T("tmpl.aliases"),
		"@@EXAMPLES@@", i18n.T("tmpl.examples"),
		"@@AVAILABLE@@", i18n.T("tmpl.available"),
		"@@ADDTOPICS@@", i18n.T("tmpl.additionalTopics"),
		"@@GLOBALFLAGS@@", i18n.T("tmpl.globalFlags"),
		"@@FLAGS@@", i18n.T("tmpl.flags"),
		"@@HELPPREFIX@@", i18n.T("tmpl.helpPrefix"),
		"@@HELPSUFFIX@@", i18n.T("tmpl.helpSuffix"),
	).Replace(`@@USAGE@@{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

@@ALIASES@@
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

@@EXAMPLES@@
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}

@@AVAILABLE@@{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

@@FLAGS@@
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

@@GLOBALFLAGS@@
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

@@ADDTOPICS@@{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

@@HELPPREFIX@@{{.CommandPath}} [command] --help@@HELPSUFFIX@@{{end}}
`)
}

// exactArgs / minArgs / rangeArgs localize cobra's positional-argument usage
// errors.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("%s", i18n.T("err.exactArgs", "Cmd", cmd.Name(), "N", n))
		}
		return nil
	}
}

func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return fmt.Errorf("%s", i18n.T("err.minArgs", "Cmd", cmd.Name(), "N", n))
		}
		return nil
	}
}

func rangeArgs(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < min || len(args) > max {
			return fmt.Errorf("%s", i18n.T("err.rangeArgs", "Cmd", cmd.Name(), "Min", min, "Max", max))
		}
		return nil
	}
}

// printTable renders a header row plus body rows through a tabwriter.
func printTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

// yesNo renders a boolean table cell in the active locale.
func yesNo(b bool) string {
	if b {
		return i18n.T("search.yes")
	}
	return i18n.T("search.no")
}

// formatMoney renders an amount in cents with a currency symbol; unknown
// currencies fall back to the ISO code. The server sends lowercase codes on
// the wire; both cases are accepted.
func formatMoney(cents int64, currency string) string {
	symbols := map[string]string{
		"EUR": "€",
		"USD": "$",
		"GBP": "£",
		"CHF": "CHF ",
		"CAD": "CA$",
		"AUD": "A$",
	}
	sym, ok := symbols[strings.ToUpper(currency)]
	if !ok {
		sym = strings.ToUpper(currency) + " "
	}
	return fmt.Sprintf("%s%.2f", sym, float64(cents)/100)
}

// dollarsToCents converts an upstream decimal dollar figure to whole cents
// with round-half-away-from-zero: a bare int64(f*100) truncates
// 19.99*100 = 1998.999… to 1998.
func dollarsToCents(f float64) int64 {
	return int64(math.Round(f * 100))
}

// atoi64 parses a record id.
func atoi64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
