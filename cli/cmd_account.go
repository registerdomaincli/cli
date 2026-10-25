package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// accountView is the machine-readable shape of a saved sign-in. The token is
// deliberately absent: `account list --json` is meant to be piped, and every
// entry's Token is a live full-privilege credential.
type accountView struct {
	Email     string `json:"email"`
	ServerURL string `json:"server_url"`
	Active    bool   `json:"active"`
}

// newSignupCmd and newLoginCmd are one flow under two names. The server answers
// /v1/auth/signup identically whether or not the address is known (no account
// enumeration) and /v1/auth/verify mints the token either way, so "signup" is
// what a new visitor looks for and "login" is what a returning one types. Both
// save the sign-in as a switchable account.
func newSignupCmd(app *App) *cobra.Command {
	return newAuthCmd(app, "signup", "signup.short", "signup.long")
}

func newLoginCmd(app *App) *cobra.Command {
	return newAuthCmd(app, "login", "login.short", "login.long")
}

func newAuthCmd(app *App, use, shortKey, longKey string) *cobra.Command {
	var email, code, codeFile string
	cmd := &cobra.Command{
		Use:   use,
		Short: i18n.T(shortKey),
		Long:  i18n.T(longKey),
		Args:  exactArgs(0),
		// /v1/auth/signup and /v1/auth/verify are public: this command needs no
		// credential and must not borrow the stored one.
		Annotations: map[string]string{annotationNoRootToken: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			// Legal notice with links into the resolved server. The auth
			// endpoints are the only ones that work without a token. Under
			// --json stdout carries the response object and nothing else.
			if !jsonFlag {
				fmt.Fprintln(out, i18n.T("cli.legal.notice",
					"TermsURL", app.Server+"/legal/terms",
					"PrivacyURL", app.Server+"/legal/privacy"))
			}

			// --code-file keeps the 6-digit code out of the process table and
			// shell history, as --token-file does for the API token.
			if code == "" && codeFile != "" {
				data, err := os.ReadFile(codeFile)
				if err != nil {
					return fmt.Errorf("cannot read %s: %w", codeFile, err)
				}
				code = strings.TrimSpace(string(data))
			}

			// Parametric first: --email and --code cover the whole flow.
			// login --email a@b.c            → request the code, exit
			// login --email a@b.c --code 123 → verify + save the token
			// Without flags and attached to a terminal the prompts remain.
			if email == "" {
				if !app.p.stdinIsTTY() {
					return missingFlags("--email")
				}
				email = app.p.required(i18n.T("signup.emailPrompt"))
			}
			if code == "" {
				// Step 1: request the verification code. Exit 0 — the human
				// reads it from the inbox, then reruns with --code. Step 2
				// verifies only: a second signup POST would mint a fresh code
				// and invalidate the one being typed.
				if _, err := app.Client.Signup(email); err != nil {
					return fail(err)
				}
				if jsonFlag {
					return emitJSON(cmd, map[string]string{"status": "sent", "email": email})
				}
				fmt.Fprintln(out, i18n.T("signup.sent", "Email", email))
				return nil
			}
			// No "code sent" line here: this invocation verifies the code the
			// first one requested. Claiming a send would be false.
			// Read the store before verifying. Verify mints a live token, and a
			// store that cannot be read would strand it: minted, never saved,
			// never shown to the caller, and impossible to revoke. (logout is
			// the other way round on purpose — revoking first leaves at worst a
			// dead token in a store it then fails to clean.)
			cfg, err := Load()
			if err != nil {
				return err
			}
			token, err := app.Client.Verify(email, code)
			if err != nil {
				return fail(err)
			}
			// Everything this sign-in displaces. Normally that is the saved
			// entry for the email; on a config written before accounts existed
			// it is the live session itself, which UpsertAccount is about to
			// overwrite and which no entry would then record — leaving a live
			// full-privilege token with nothing pointing at it. The two are
			// independent: a hand-edited config can hold an entry for this
			// email *and* an unrecorded live session.
			var superseded []Account
			if prev, ok := cfg.FindAccount(email); ok {
				superseded = append(superseded, prev)
			}
			// Independent, but not disjoint: the same token can appear as this
			// email's entry and as the live session under a different
			// server_url. Revoking it once per entry would send the credential
			// to a host that never issued it, which is what the config's own
			// pairing exists to prevent. One token, one revocation, addressed
			// to the server the entry recorded.
			if _, recorded := cfg.ActiveAccount(); cfg.Token != "" && !recorded &&
				(len(superseded) == 0 || superseded[0].Token != cfg.Token) {
				superseded = append(superseded, Account{ServerURL: cfg.ServerURL, Token: cfg.Token})
			}
			// Saved as an account, not merely as the live session: signing in
			// again later must not cost another email round trip.
			cfg.UpsertAccount(email, app.Server, token)
			if err := Save(cfg); err != nil {
				// Verify already minted this token on the server, and the
				// caller will never see it now. Revoke it rather than strand a
				// live credential with nothing pointing at it.
				if rerr := revoke(app.Server, token); rerr != nil {
					warnNotRevoked(cmd, app.Server, rerr)
				}
				// The write error alone sends the caller back to a code that
				// no longer works: Verify spent it, so the obvious retry fails
				// a second time with nothing to explain why.
				return fmt.Errorf("%w\n%s", err, i18n.T("signup.discarded", "Email", email))
			}
			// Revoke only once the replacement is durable. Revoking first
			// would leave this machine with no working credential at all if
			// the write failed: the old token dead, the new one unsaved.
			for _, prev := range superseded {
				if prev.Token != token {
					if rerr := revoke(prev.ServerURL, prev.Token); rerr != nil {
						warnNotRevoked(cmd, prev.ServerURL, rerr)
					}
				}
			}
			path, _ := ConfigPath()
			if jsonFlag {
				return emitJSON(cmd, map[string]string{"token": token, "configPath": path})
			}
			fmt.Fprintln(out, i18n.T("signup.saved", "Path", path))
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	cmd.Flags().StringVar(&code, "code", "", "6-digit verification code from the email (with --email: complete sign-in in one shot)")
	cmd.Flags().StringVar(&codeFile, "code-file", "", "read the 6-digit verification code from a file (used when --code is absent)")
	return cmd
}

// newLogoutCmd ends the session on this machine: the stored token is revoked
// on the server, then forgotten locally.
func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: i18n.T("logout.short"),
		Long:  i18n.T("logout.long"),
		Args:  exactArgs(0),
		// With no session there is nothing to revoke, so this must run on a
		// machine that has never named a server.
		Annotations: map[string]string{annotationLocal: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if app.Token == "" {
				// "No token" and "the store could not be read" look identical
				// from app.Token alone. Reporting a clean sign-out for the
				// second would leave a live credential on disk while telling
				// the caller there was nothing to do.
				if _, err := Load(); err != nil {
					return err
				}
				if jsonFlag {
					return emitJSON(cmd, map[string]string{"status": "signed_out", "email": ""})
				}
				fmt.Fprintln(out, i18n.T("logout.none"))
				return nil
			}
			// Revoke before forgetting. Dropping the token locally while it
			// stays valid on the server would report a logout that did not
			// happen; a 401 means it is already dead, which is the goal.
			if err := app.Client.Logout(); err != nil && !alreadyGone(err) {
				return fail(err)
			}
			email := ""
			// Only a session that came from the config is forgotten there. A
			// credential named by --token/--token-file/DOMAIN_CLI_TOKEN is the
			// caller's, not the file's: revoke it, leave the file alone.
			if app.tokenFromConfig {
				// Read here rather than up front: this is the read whose result
				// is written back, so its failure must abort. Saving the zero
				// value would drop every saved sign-in.
				cfg, err := Load()
				if err != nil {
					return err
				}
				if a, ok := cfg.ActiveAccount(); ok {
					email = a.Email
					cfg.RemoveAccount(a.Email)
				}
				cfg.Token = ""
				if err := Save(cfg); err != nil {
					return err
				}
			}
			if jsonFlag {
				return emitJSON(cmd, map[string]string{"status": "signed_out", "email": email})
			}
			if email != "" {
				fmt.Fprintln(out, i18n.T("logout.doneEmail", "Email", email))
			} else {
				fmt.Fprintln(out, i18n.T("logout.done"))
			}
			return nil
		},
	}
}

func newWhoamiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: i18n.T("whoami.short"),
		Long:  i18n.T("whoami.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			me, err := c.Me()
			if err != nil {
				return fail(err)
			}
			if jsonFlag {
				return emitJSON(cmd, me)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("whoami.ok", "Email", me.Email))
			return nil
		},
	}
}

func newAccountCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: i18n.T("account.short"),
		Long:  i18n.T("account.long"),
	}
	cmd.AddCommand(newAccountListCmd(app))
	cmd.AddCommand(newAccountSwitchCmd(app))
	cmd.AddCommand(newAccountRemoveCmd(app))
	cmd.AddCommand(newAccountDeleteCmd(app))
	return cmd
}

// newAccountListCmd prints the saved sign-ins and which one is live.
func newAccountListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: i18n.T("account.list.short"),
		Long:  i18n.T("account.list.long"),
		Args:  exactArgs(0),
		// Purely local: it reads the config and touches no endpoint.
		Annotations: map[string]string{
			annotationLocal:       "true",
			annotationNoRootToken: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := Load()
			if err != nil {
				return err
			}
			// The live session decides which entry is "in use", not the config
			// alone: --token/DOMAIN_CLI_TOKEN change what commands act as. A
			// config-backed token belongs to the server recorded beside it, so
			// an unrelated --server override must not blank the marker.
			liveServer := app.Server
			if app.tokenFromConfig {
				liveServer = cfg.ServerURL
			}
			live := func(a Account) bool {
				return app.Token != "" && a.Token == app.Token && sameServer(a.ServerURL, liveServer)
			}
			if jsonFlag {
				views := make([]accountView, 0, len(cfg.Accounts))
				for _, a := range cfg.Accounts {
					views = append(views, accountView{Email: a.Email, ServerURL: a.ServerURL, Active: live(a)})
				}
				return emitJSON(cmd, views)
			}
			if len(cfg.Accounts) == 0 {
				fmt.Fprintln(out, i18n.T("account.list.empty"))
				return nil
			}
			rows := make([][]string, 0, len(cfg.Accounts))
			for _, a := range cfg.Accounts {
				rows = append(rows, []string{a.Email, a.ServerURL, yesNo(live(a))})
			}
			printTable(out, []string{
				i18n.T("account.list.hdr.email"),
				i18n.T("account.list.hdr.server"),
				i18n.T("account.list.hdr.active"),
			}, rows)
			return nil
		},
	}
}

// newAccountSwitchCmd makes a saved account the live session. The token is
// probed first: landing in a session the server has already revoked would look
// like a successful switch and only fail on the next command.
func newAccountSwitchCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <email>",
		Short: i18n.T("account.switch.short"),
		Long:  i18n.T("account.switch.long"),
		Args:  exactArgs(1),
		// Builds its own client from the selected entry, so the root
		// credential and the root server are both unused.
		Annotations: map[string]string{
			annotationLocal:       "true",
			annotationNoRootToken: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := Load()
			if err != nil {
				return err
			}
			a, ok := cfg.FindAccount(args[0])
			if !ok {
				return errors.New(i18n.T("account.notFound", "Email", args[0]))
			}
			if err := ValidateServerURL(a.ServerURL); err != nil {
				return err
			}
			if _, err := NewClient(a.ServerURL, a.Token, i18n.Current()).Me(); err != nil {
				if alreadyGone(err) {
					return errors.New(i18n.T("account.switch.stale", "Email", a.Email))
				}
				return fail(err)
			}
			cfg.SetActive(a.Email)
			if err := Save(cfg); err != nil {
				return err
			}
			if jsonFlag {
				return emitJSON(cmd, map[string]string{
					"status": "switched", "email": a.Email, "server_url": a.ServerURL,
				})
			}
			fmt.Fprintln(out, i18n.T("account.switch.done", "Email", a.Email))
			return nil
		},
	}
}

// newAccountRemoveCmd revokes a saved account's token and forgets it, whether
// or not it is the live one.
func newAccountRemoveCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <email>",
		Short: i18n.T("account.remove.short"),
		Long:  i18n.T("account.remove.long"),
		Args:  exactArgs(1),
		// Builds its own client from the selected entry, so the root
		// credential and the root server are both unused.
		Annotations: map[string]string{
			annotationLocal:       "true",
			annotationNoRootToken: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := Load()
			if err != nil {
				return err
			}
			a, ok := cfg.FindAccount(args[0])
			if !ok {
				return errors.New(i18n.T("account.notFound", "Email", args[0]))
			}
			// Revoke before forgetting, as logout does: a forgotten entry must
			// not leave a usable token behind. A 401 means it is already dead.
			if err := ValidateServerURL(a.ServerURL); err != nil {
				return err
			}
			if err := NewClient(a.ServerURL, a.Token, i18n.Current()).Logout(); err != nil && !alreadyGone(err) {
				return fail(err)
			}
			cfg.RemoveAccount(a.Email)
			if err := Save(cfg); err != nil {
				return err
			}
			if jsonFlag {
				return emitJSON(cmd, map[string]string{"status": "removed", "email": a.Email})
			}
			fmt.Fprintln(out, i18n.T("account.remove.done", "Email", a.Email))
			return nil
		},
	}
}

func newAccountDeleteCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "delete",
		Short: i18n.T("account.delete.short"),
		Long:  i18n.T("account.delete.long"),
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.requireClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			// Typed confirmation interactively; global --yes non-interactively.
			if !yesFlag {
				if !app.p.stdinIsTTY() {
					return missingFlags("--yes")
				}
				answer := app.p.ask(i18n.T("account.delete.confirmPrompt"))
				if strings.TrimSpace(answer) != "DELETE" {
					if jsonFlag {
						return emitJSON(cmd, map[string]string{"status": "aborted"})
					}
					fmt.Fprintln(out, i18n.T("account.delete.aborted"))
					return nil
				}
			}
			if err := fail(c.DeleteMe()); err != nil {
				return err
			}
			// The account is gone server-side, so the saved sign-in is
			// worthless. Only the config's own session is touched: a
			// credential named by --token/DOMAIN_CLI_TOKEN may point at a
			// different account, and the file is not ours to rewrite.
			if app.tokenFromConfig {
				cfg, err := Load()
				if err != nil {
					return err
				}
				if a, ok := cfg.ActiveAccount(); ok {
					cfg.RemoveAccount(a.Email)
				}
				cfg.Token = ""
				_ = Save(cfg) // best effort; the account no longer exists
			}
			if jsonFlag {
				return emitJSON(cmd, map[string]string{"status": "deleted"})
			}
			fmt.Fprintln(out, i18n.T("account.delete.done"))
			return nil
		},
	}
}

// revoke revokes a token this CLI is replacing and reports the outcome. A
// token the server has already rejected is the goal met, not a failure; a
// server URL the CLI may not send a credential to is, since the token then
// stays live with no way to reach it from here.
func revoke(server, token string) error {
	if token == "" {
		return nil
	}
	if err := ValidateServerURL(server); err != nil {
		return err
	}
	if err := NewClient(server, token, i18n.Current()).Logout(); err != nil && !alreadyGone(err) {
		return err
	}
	return nil
}

// warnNotRevoked reports a revocation the sign-in could not complete. Every
// token revoked on this path is one the config has stopped recording — the new
// one because the write failed, a superseded one because UpsertAccount
// replaced the entry that named it — so a silent failure leaves a live
// full-privilege credential nothing on this machine can reach again. It warns
// rather than aborts: by then the sign-in is either already durable or already
// being reported as failed, and neither is improved by hiding this.
//
// The detail goes through fail: a 429 arrives as a RateLimitedError whose
// Error() is hard-coded English, and it would otherwise be pasted into an
// otherwise localized sentence. An APIError passes through untouched — the
// client sends Accept-Language, so the server already phrased it.
func warnNotRevoked(cmd *cobra.Command, server string, err error) {
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("warn.tokenNotRevoked", "Server", server, "Err", fail(err)))
}

// alreadyGone reports whether the server answered 401 — the credential is
// already dead, so the caller's goal is met.
func alreadyGone(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}
