package cli

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The agent contract: EVERY interactive point has a parameter path. When the
// required parameters are absent and stdin is not a terminal, commands fail
// with err.missingFlags naming the flags to pass — they never hang and never
// silently fall back. Interactive prompts remain only for humans (stdin is a
// TTY and no flag was given).

// jsonFlag is the global --json flag: machine-readable output for agents.
var jsonFlag bool

// yesFlag is the global --yes: it answers every confirmation (order
// placement, autorenew opt-in, account deletion) without a prompt. The
// human-in-the-loop stays explicit: an agent passes --yes only after the
// human approved.
var yesFlag bool

// stdinIsTTY reports whether the command's input source is an interactive
// terminal. Piped/redirected stdin (agents, CI, tests) means: no prompts,
// parameters only. The check is on the prompter's reader (cobra's In), not
// process stdin, so tests that substitute the stream behave like agents.
//
// It asks the kernel rather than inspecting the file mode: /dev/null is a
// character device too, and treating it as a terminal made the CLI prompt a
// caller that can never answer — the confirmation read EOF, "aborted", and
// the command exited 0 without naming the flag.
func (p *prompter) stdinIsTTY() bool {
	f, ok := p.src.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// stdoutIsTTY reports whether output goes to a terminal (drives whether
// opening a browser makes sense).
func stdoutIsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// missingFlags is the single non-interactive failure mode: name the flags
// the caller must pass.
func missingFlags(fl ...string) error {
	return errors.New(i18n.T("err.missingFlags", "Flags", strings.Join(fl, ", ")))
}

// requireYes enforces the human-approval gate in non-interactive runs: an
// agent must pass the global --yes explicitly. Interactive sessions still
// get the prompt.
func requireYes(cmd *cobra.Command, app *App) (bool, error) {
	if yesFlag {
		return true, nil
	}
	if !app.p.stdinIsTTY() {
		return false, missingFlags("--yes")
	}
	return app.p.confirm(""), nil
}

// registrantFlags carries the parametric registrant fields shared by
// register/renew/transfer and contacts add.
type registrantFlags struct {
	FirstName    string
	LastName     string
	Organization string
	Email        string
	Phone        string
	Address1     string
	Address2     string
	City         string
	State        string
	Zip          string
	Country      string
}

func addRegistrantFlags(cmd *cobra.Command, r *registrantFlags) {
	cmd.Flags().StringVar(&r.FirstName, "first-name", "", "registrant first name")
	cmd.Flags().StringVar(&r.LastName, "last-name", "", "registrant last name")
	cmd.Flags().StringVar(&r.Organization, "organization", "", "registrant organization (optional)")
	cmd.Flags().StringVar(&r.Email, "email", "", "registrant email (defaults to the account email)")
	cmd.Flags().StringVar(&r.Phone, "phone", "", "registrant phone")
	cmd.Flags().StringVar(&r.Address1, "address1", "", "registrant address line 1")
	cmd.Flags().StringVar(&r.Address2, "address2", "", "registrant address line 2 (optional)")
	cmd.Flags().StringVar(&r.City, "city", "", "registrant city")
	cmd.Flags().StringVar(&r.State, "state", "", "registrant state/province (optional)")
	cmd.Flags().StringVar(&r.Zip, "zip", "", "registrant postal code")
	cmd.Flags().StringVar(&r.Country, "country", "", "registrant country (ISO 3166-1 alpha-2)")
}

// any reports whether any registrant flag was passed — that switches the
// command into fully-parametric mode.
func (r *registrantFlags) any() bool {
	return r.FirstName != "" || r.LastName != "" || r.Organization != "" ||
		r.Email != "" || r.Phone != "" || r.Address1 != "" || r.Address2 != "" ||
		r.City != "" || r.State != "" || r.Zip != "" || r.Country != ""
}

// build validates the flag-provided registrant and returns an error naming
// every missing required field (never a partial silent submit).
func (r *registrantFlags) build() (*apitypes.Registrant, error) {
	var missing []string
	if r.FirstName == "" {
		missing = append(missing, "--first-name")
	}
	if r.LastName == "" {
		missing = append(missing, "--last-name")
	}
	if r.Email == "" {
		missing = append(missing, "--email")
	}
	if r.Phone == "" {
		missing = append(missing, "--phone")
	}
	if r.Address1 == "" {
		missing = append(missing, "--address1")
	}
	if r.City == "" {
		missing = append(missing, "--city")
	}
	if r.Zip == "" {
		missing = append(missing, "--zip")
	}
	if r.Country == "" {
		missing = append(missing, "--country")
	}
	if len(missing) > 0 {
		return nil, missingFlags(missing...)
	}
	if len(r.Country) != 2 {
		return nil, errors.New(i18n.T("err.invalidCountry"))
	}
	return &apitypes.Registrant{
		FirstName:    r.FirstName,
		LastName:     r.LastName,
		Organization: r.Organization,
		Email:        r.Email,
		Phone:        r.Phone,
		Address1:     r.Address1,
		Address2:     r.Address2,
		City:         r.City,
		State:        r.State,
		Zip:          r.Zip,
		Country:      strings.ToUpper(r.Country),
	}, nil
}

// emitJSON marshals v as the command's entire output when --json is active.
func emitJSON(cmd *cobra.Command, v any) error {
	if !jsonFlag {
		return nil
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
