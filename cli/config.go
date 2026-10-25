package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/registerdomaincli/cli/cli/i18n"
)

// Config is the persisted CLI state, stored at
// os.UserConfigDir()/domain-cli/config.json as
// {"server_url","token","accounts"}. ServerURL/Token are the live session;
// Accounts are the saved sign-ins `account switch` moves between. Currency
// (ISO 4217, lowercase) is the preferred billing currency for quotes and
// orders; empty means the server chooses its default.
// The file is written 0600 and its directory 0700 because it carries a
// full-privilege API token.
type Config struct {
	ServerURL string    `json:"server_url"`
	Token     string    `json:"token"`
	Currency  string    `json:"currency,omitempty"`
	Accounts  []Account `json:"accounts,omitempty"`
}

// Account is one saved sign-in: the token minted for an email at a server.
// Several are kept so switching accounts costs nothing — every sign-in mints
// its own token and leaves the earlier ones valid, so a saved entry stays
// usable until it is explicitly revoked. Exactly one account is live, mirrored
// in Config.ServerURL/Token so the rest of the CLI still reads one session.
type Account struct {
	Email     string `json:"email"`
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

// userConfigDir is indirected for tests.
var userConfigDir = os.UserConfigDir

// ConfigDir returns the directory holding config.json.
func ConfigDir() (string, error) {
	base, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the user config directory: %w", err)
	}
	return filepath.Join(base, "domain-cli"), nil
}

// ConfigPath returns the full path of the config file.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file. A missing file yields the zero Config, not an
// error — first run is a normal state.
func Load() (Config, error) {
	var cfg Config
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config file %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config file 0600 inside a 0700 directory.
//
// MkdirAll leaves an existing directory's mode alone and os.WriteFile applies
// its perm argument only when it creates the file, so a config restored from a
// tarball, written by an older build, or left in a group-writable home could
// keep the token readable by others. Both modes are re-asserted here.
//
// The write goes through a temp file renamed into place: a pre-planted
// config.json symlink is replaced instead of followed, and an interrupted
// write can no longer truncate the stored token.
func Save(cfg Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("cannot secure %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	// CreateTemp opens 0600 (umask can only narrow it), so the temp file is
	// never readable by others even before the rename.
	tmp, err := os.CreateTemp(dir, "config.json.tmp")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// ValidateServerURL rejects an API endpoint the CLI must not send the bearer
// token to: anything but https, or plain http for a loopback host. The token
// is a full-privilege registrar credential and travels in the Authorization
// header of every request, so a server named by --server or
// DOMAIN_CLI_SERVER_URL must not be able to downgrade it to cleartext.
// It applies the same policy as the browser opener.
func ValidateServerURL(raw string) error {
	if !validExternalURL(raw) {
		return errors.New(i18n.T("err.badServer", "URL", raw))
	}
	return nil
}

// tokenBelongsToServer reports whether the resolved server and token may be
// used together. A token read from the config file belongs to the server
// recorded next to it; sending it to a host named by --server or
// DOMAIN_CLI_SERVER_URL would hand the credential to whoever controls that
// host. An explicit --token or DOMAIN_CLI_TOKEN means the caller chose the
// pair deliberately and is taken at its word.
func tokenBelongsToServer(explicitToken string, cfg Config, server string) bool {
	if explicitToken != "" || os.Getenv("DOMAIN_CLI_TOKEN") != "" {
		return true
	}
	if cfg.Token == "" || cfg.ServerURL == "" {
		return true
	}
	return sameServer(server, cfg.ServerURL)
}

// sameServer compares two server URLs ignoring a trailing slash, which
// NewClient trims anyway.
func sameServer(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// FindAccount returns the saved account for an email. Emails are matched
// case-insensitively: the server stores them lower-cased, a human typing one
// is not.
func (c Config) FindAccount(email string) (Account, bool) {
	for _, a := range c.Accounts {
		if strings.EqualFold(a.Email, email) {
			return a, true
		}
	}
	return Account{}, false
}

// ActiveAccount returns the saved account the live session belongs to. It is
// absent when the session came from --token/DOMAIN_CLI_TOKEN, or from a config
// written before accounts existed.
func (c Config) ActiveAccount() (Account, bool) {
	if c.Token == "" {
		return Account{}, false
	}
	for _, a := range c.Accounts {
		if a.Token == c.Token && sameServer(a.ServerURL, c.ServerURL) {
			return a, true
		}
	}
	return Account{}, false
}

// UpsertAccount saves a sign-in and makes it the live session. Signing in
// again to a known email replaces that entry instead of stacking a second one:
// the new login mints a new token, and the superseded one is worthless to
// keep. The email is the key, so a sign-in to a different server replaces the
// entry too — one saved sign-in per address, on whichever server was named
// last.
func (c *Config) UpsertAccount(email, server, token string) {
	c.ServerURL, c.Token = server, token
	for i := range c.Accounts {
		if strings.EqualFold(c.Accounts[i].Email, email) {
			c.Accounts[i] = Account{Email: email, ServerURL: server, Token: token}
			return
		}
	}
	c.Accounts = append(c.Accounts, Account{Email: email, ServerURL: server, Token: token})
}

// SetActive points the live session at a saved account.
func (c *Config) SetActive(email string) bool {
	a, ok := c.FindAccount(email)
	if !ok {
		return false
	}
	c.ServerURL, c.Token = a.ServerURL, a.Token
	return true
}

// carriesToken reports whether the config holds any credential at all — the
// live session's or a dormant account's. A config whose session was signed out
// still carries every other account's token, so a permission check that looks
// only at Token would miss them.
func (c Config) carriesToken() bool {
	if c.Token != "" {
		return true
	}
	for _, a := range c.Accounts {
		if a.Token != "" {
			return true
		}
	}
	return false
}

// RemoveAccount forgets a saved account. Dropping the live one clears the
// session token too, so no credential outlives the entry that named it; the
// server URL stays, so the next command is not asked for it again.
func (c *Config) RemoveAccount(email string) bool {
	kept := make([]Account, 0, len(c.Accounts))
	removed := false
	for _, a := range c.Accounts {
		if strings.EqualFold(a.Email, email) {
			removed = true
			if a.Token == c.Token && sameServer(a.ServerURL, c.ServerURL) {
				c.Token = ""
			}
			continue
		}
		kept = append(kept, a)
	}
	c.Accounts = kept
	return removed
}

// ResolveServer applies the precedence --server flag > DOMAIN_CLI_SERVER_URL
// env > config file. The result may be empty (first run).
func ResolveServer(flagServer string, cfg Config) string {
	if flagServer != "" {
		return flagServer
	}
	if env := os.Getenv("DOMAIN_CLI_SERVER_URL"); env != "" {
		return env
	}
	return cfg.ServerURL
}

// ResolveToken applies the precedence --token flag > DOMAIN_CLI_TOKEN env >
// config file. The result may be empty (not signed in).
func ResolveToken(flagToken string, cfg Config) string {
	if flagToken != "" {
		return flagToken
	}
	if env := os.Getenv("DOMAIN_CLI_TOKEN"); env != "" {
		return env
	}
	return cfg.Token
}

// ResolveCurrency applies the precedence --currency flag >
// DOMAIN_CLI_CURRENCY env > config file. The result may be empty (the server
// then bills in its own default). The value is lowercased because the wire
// and the server's supported-currency list are lowercase.
func ResolveCurrency(flagCurrency string, cfg Config) string {
	c := flagCurrency
	if c == "" {
		c = os.Getenv("DOMAIN_CLI_CURRENCY")
	}
	if c == "" {
		c = cfg.Currency
	}
	return strings.ToLower(strings.TrimSpace(c))
}

// DefaultServerURL is the production server; DOMAIN_CLI_ENV=dev selects the
// local development default used for the first-run prompt.
func DefaultServerURL() string {
	if os.Getenv("DOMAIN_CLI_ENV") == "dev" {
		return "http://localhost:8080"
	}
	return "https://registerdomaincli.com"
}
