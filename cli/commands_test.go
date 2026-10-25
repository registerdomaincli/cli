package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
)

// setLocale en restores the default locale after tests that change it.
func setLocale(t *testing.T, code string) {
	t.Helper()
	i18n.SetLocale(code)
	t.Cleanup(func() { i18n.SetLocale("en") })
}

// runRoot executes the CLI against args with the given stdin and returns
// everything written to stdout/stderr.
func runRoot(t *testing.T, args []string, stdin string) string {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v\noutput:\n%s", args, err, out.String())
	}
	return out.String()
}

// runRootErr executes the CLI against args expecting failure, and returns
// everything written to stdout/stderr — cobra prints the error there itself.
func runRootErr(t *testing.T, args []string) string {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	if err := root.Execute(); err == nil {
		t.Fatalf("execute %v: expected an error, output:\n%s", args, out.String())
	}
	return out.String()
}

// breakConfigDir makes Save fail by replacing the config directory with a
// regular file, which MkdirAll cannot turn back into a directory. Called from
// a request handler it breaks the store mid-flight, between the read the
// sign-in does up front and the write it does at the end.
func breakConfigDir(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "domain-cli")
	if err := os.RemoveAll(path); err != nil {
		t.Errorf("removeall: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Errorf("write blocker: %v", err)
	}
}

// TestSignupVerifySavesToken runs signup against a fake server and checks
// the token lands in the config file with 0600 permissions.
func TestSignupVerifySavesToken(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1") // never open a browser from a test
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/signup":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(apitypes.SignupResponse{Status: "sent"})
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_saved"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	// Parametric: --email + --code complete signup in one shot, no TTY.
	out := runRoot(t, []string{"--server", s.URL, "signup", "--email", "me@example.com", "--code", "123456"}, "")
	for _, want := range []string{"/legal/terms", "/legal/privacy", "Signed in"} {
		if !strings.Contains(out, want) {
			t.Errorf("signup output missing %q:\n%s", want, out)
		}
	}
	// This invocation verifies the code the first one requested; it sends
	// nothing, so it must not say otherwise.
	if strings.Contains(out, "code sent") {
		t.Errorf("the verify-only path claimed to have sent a code:\n%s", out)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Token != "dcl_saved" {
		t.Errorf("saved token = %q", cfg.Token)
	}
	if cfg.ServerURL != s.URL {
		t.Errorf("saved server = %q", cfg.ServerURL)
	}
	path, _ := ConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config file mode = %o, want 600", info.Mode().Perm())
	}
}

// TestRegisterDryRunPrintsQuote checks the quote-first confirm line and that
// --dry-run stops before creating an order.
func TestRegisterDryRunPrintsQuote(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var ordersCalled atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/quote":
			_ = json.NewEncoder(w).Encode(apitypes.Quote{Domain: "example.com", Years: 2, Kind: "registration", NetCents: 1230, TaxCents: 258, TotalCents: 1488, Currency: "EUR"})
		case r.URL.Path == "/v1/orders":
			ordersCalled.Store(true)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o1"})
		case r.URL.Path == "/v1/contact-sets":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		case r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "me@example.com"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"register", "example.com", "--years", "2", "--dry-run",
	}, "")
	if !strings.Contains(out, "example.com, 2 yr — €12.30 + VAT = €14.88. Continue? [y/N]") {
		t.Errorf("confirm line missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "Dry run: no order was created.") {
		t.Errorf("dry-run marker missing:\n%s", out)
	}
	if ordersCalled.Load() {
		t.Error("--dry-run must not create an order")
	}
}

// TestOrderPollTransitions walks the fake server through paid → processing →
// completed and asserts the printed transitions and invoice number.
func TestOrderPollTransitions(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	oldInterval, oldTimeout := pollInterval, pollTimeout
	pollInterval = time.Millisecond
	pollTimeout = 5 * time.Second
	t.Cleanup(func() { pollInterval, pollTimeout = oldInterval, oldTimeout })

	var polls atomic.Int32
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/quote":
			_ = json.NewEncoder(w).Encode(apitypes.Quote{Domain: "example.com", Years: 1, Kind: "renewal", NetCents: 900, TaxCents: 189, TotalCents: 1089, Currency: "EUR"})
		case r.URL.Path == "/v1/orders" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o9", Status: "pending_payment"})
		case r.URL.Path == "/v1/orders/o9":
			// First poll: paid (invoice assigned); second: processing; then done.
			switch n := polls.Add(1); n {
			case 1:
				_ = json.NewEncoder(w).Encode(apitypes.Order{
					ID: "o9", Status: "paid", InvoiceNumber: "A-2026-000042",
				})
			case 2:
				_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o9", Status: "processing"})
			default:
				_ = json.NewEncoder(w).Encode(apitypes.Order{
					ID: "o9", Status: "completed", InvoiceNumber: "A-2026-000042",
				})
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "renew", "example.com", "--yes"}, "")
	for _, want := range []string{
		"Status: paid",
		"Invoice: A-2026-000042",
		"Status: processing",
		"Status: completed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("poll output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Status: pending_payment") {
		t.Errorf("creation status should not print twice:\n%s", out)
	}
}

// TestRenewDeclineNonInteractive checks that without --yes on a non-TTY
// stdin no order is created — the human-approval gate fails naming the flag.
func TestRenewDeclineNonInteractive(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var ordered atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/quote" {
			_ = json.NewEncoder(w).Encode(apitypes.Quote{Domain: "example.com", Years: 1, Kind: "renewal", NetCents: 900, TaxCents: 189, TotalCents: 1089, Currency: "EUR"})
			return
		}
		if r.URL.Path == "/v1/orders" {
			ordered.Store(true)
		}
		w.WriteHeader(http.StatusNotFound)
	})
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("n\n")) // piped decline is not interactive consent
	root.SetArgs([]string{"--server", s.URL, "--token", "dcl_x", "renew", "example.com"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected missing --yes error")
	}
	if strings.Contains(out.String(), "order.created") || ordered.Load() {
		t.Error("no order may be created without --yes")
	}
	if ordered.Load() {
		t.Error("declined confirm must not create an order")
	}
}

// TestTransferRequiresAuthCode checks the localized usage error.
func TestTransferRequiresAuthCode(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"--server", "http://127.0.0.1:1", "--token", "dcl_x", "transfer", "example.com"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected auth-code error")
	}
	if !strings.Contains(out.String(), "--auth-code") {
		t.Errorf("missing auth-code error not localized as expected:\n%s", out.String())
	}
}

// TestConfigFilePermAfterSignupAlreadyCovered; this test pins the 0700 dir.
func TestConfigDirPerm(t *testing.T) {
	dir := setConfigDir(t)
	if err := Save(Config{Token: "dcl_x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "domain-cli"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("config dir mode = %o, want 700", info.Mode().Perm())
	}
}

// TestRegisterFullyParametric proves the agent contract: registrant flags +
// --yes + --no-wait create an order with zero interactive input (stdin is a
// pipe, not a TTY), the order JSON is emitted, and no payment is attempted.
func TestRegisterFullyParametric(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var gotOrder apitypes.CreateOrderRequest
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/quote":
			_ = json.NewEncoder(w).Encode(apitypes.Quote{
				Domain: "example.com", Years: 1, Kind: "registration",
				NetCents: 1150, Currency: "EUR",
			})
		case r.URL.Path == "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(&gotOrder)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apitypes.Order{
				ID: "ord-1", Kind: "registration", Domain: "example.com",
				Status: "pending_payment", CheckoutURL: "http://checkout.example/1",
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--json", "--yes",
		"register", "example.com",
		"--first-name", "A", "--last-name", "B",
		"--email", "agent@example.com", "--phone", "+34.600000000",
		"--address1", "Street 1", "--city", "BCN", "--zip", "08001",
		"--country", "es", "--no-wait",
	}, "")
	if gotOrder.Registrant == nil || gotOrder.Registrant.FirstName != "A" {
		t.Fatalf("order registrant = %+v", gotOrder.Registrant)
	}
	if !strings.Contains(out, "ord-1") || !strings.Contains(out, "http://checkout.example/1") {
		t.Errorf("json output missing order fields:\n%s", out)
	}
}

// TestRegisterNonInteractiveMissingFlags proves the non-interactive failure
// mode: without registrant flags and without a TTY, the command fails naming
// the flags to pass — it never hangs and never submits partial data.
func TestRegisterNonInteractiveMissingFlags(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/contact-sets", "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("")) // pipe, not a TTY
	root.SetArgs([]string{"--server", s.URL, "--token", "dcl_x", "register", "example.com"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected missing-flags error")
	}
	for _, flag := range []string{"--first-name", "--email", "--country"} {
		if !strings.Contains(out.String(), flag) {
			t.Errorf("error missing %q:\n%s", flag, out.String())
		}
	}
}

// TestAutorenewRequiresYesInNonInteractive proves the off-session charge
// opt-in gate: without --yes on a non-TTY stdin the command fails.
func TestAutorenewRequiresYesInNonInteractive(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"--server", s.URL, "--token", "dcl_x", "domains", "autorenew", "example.com", "on"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected --yes error")
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Errorf("error must name --yes:\n%s", out.String())
	}
}

// TestTransferAwayRequiresYesInNonInteractive proves the gate on the two
// commands that together move a domain to another registrar: without --yes on
// a non-TTY stdin they fail naming the flag and nothing reaches the server.
func TestTransferAwayRequiresYesInNonInteractive(t *testing.T) {
	for _, args := range [][]string{
		{"domains", "unlock", "example.com"},
		{"domains", "authcode", "example.com"},
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			setConfigDir(t)
			setLocale(t, "en")
			var called atomic.Bool
			s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				called.Store(true)
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusOK)
			})
			var out bytes.Buffer
			root := NewRootCmd("test")
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetIn(strings.NewReader(""))
			root.SetArgs(append([]string{"--server", s.URL, "--token", "dcl_x"}, args...))
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected --yes error, got:\n%s", out.String())
			}
			if !strings.Contains(out.String(), "--yes") {
				t.Errorf("error must name --yes:\n%s", out.String())
			}
			if called.Load() {
				t.Error("the gate must fail before any request reaches the server")
			}
		})
	}
}

// TestTransferAwayWithYesProceeds proves the gate does not block the approved
// path: --yes lets both commands through to the server.
func TestTransferAwayWithYesProceeds(t *testing.T) {
	t.Run("unlock", func(t *testing.T) {
		setConfigDir(t)
		setLocale(t, "en")
		s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/v1/domains/example.com/lock" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		})
		out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "--yes", "domains", "unlock", "example.com"}, "")
		if !strings.Contains(out, "Domain example.com unlocked.") {
			t.Errorf("expected the unlock confirmation, got:\n%s", out)
		}
	})
	t.Run("authcode", func(t *testing.T) {
		setConfigDir(t)
		setLocale(t, "en")
		s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/domains/example.com/authcode" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(apitypes.AuthCodeResponse{AuthCode: "EPP-1234"})
		})
		out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "--yes", "domains", "authcode", "example.com"}, "")
		if !strings.Contains(out, "EPP-1234") {
			t.Errorf("expected the transfer code, got:\n%s", out)
		}
	})
}

// TestLockNeedsNoApproval pins the asymmetry: locking only removes
// transferability, so it stays ungated.
func TestLockNeedsNoApproval(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/domains/example.com/lock" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})
	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "domains", "lock", "example.com"}, "")
	if !strings.Contains(out, "Domain example.com locked.") {
		t.Errorf("expected the lock confirmation, got:\n%s", out)
	}
}

// TestDevNullStdinIsNonInteractive reproduces the bug the kernel-backed TTY
// check fixes: a caller that redirects stdin from /dev/null is not a
// terminal, so a gated command must fail naming the flag — not prompt a
// caller that can never answer, print "Aborted." and exit 0.
func TestDevNullStdinIsNonInteractive(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(devNull)
	root.SetArgs([]string{"--server", "https://example.com", "--token", "dcl_x", "domains", "authcode", "example.com"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the --yes error, got exit 0:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Errorf("error must name --yes:\n%s", out.String())
	}
}

// TestSavedTokenIsNotSentToAnotherServer reproduces the exfiltration the
// binding rule closes: a token stored for one server must not travel to a host
// named by --server.
func TestSavedTokenIsNotSentToAnotherServer(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	if err := Save(Config{ServerURL: "https://stored.example", Token: "dcl_stored"}); err != nil {
		t.Fatal(err)
	}
	var called atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"--server", s.URL, "whoami"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the binding error, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--token") {
		t.Errorf("error must name --token:\n%s", out.String())
	}
	if called.Load() {
		t.Error("the stored token must not reach the other server")
	}
}

// TestInvalidServerURLRejected checks the CLI refuses to send the token to a
// non-https, non-loopback endpoint.
func TestInvalidServerURLRejected(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"--server", "http://evil.example", "--token", "dcl_x", "whoami"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the server error, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "https") {
		t.Errorf("error must explain the https requirement:\n%s", out.String())
	}
}

// TestTokenFileSuppliesToken checks --token-file authenticates without putting
// the credential in argv.
func TestTokenFileSuppliesToken(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("dcl_from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotAuth string
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "user@example.com"})
	})
	out := runRoot(t, []string{"--server", s.URL, "--token-file", tokenPath, "whoami"}, "")
	if gotAuth != "Bearer dcl_from_file" {
		t.Errorf("Authorization = %q, want the trimmed file token", gotAuth)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Errorf("expected whoami output, got:\n%s", out)
	}
}

// TestLaxConfigPermissionsWarn checks the CLI tells the user when the stored
// token is readable by others.
func TestLaxConfigPermissionsWarn(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	t.Setenv("SSH_CONNECTION", "1") // keep legal from opening a browser
	if err := Save(Config{ServerURL: "https://stored.example", Token: "dcl_x"}); err != nil {
		t.Fatal(err)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRoot(t, []string{"legal"}, "")
	if !strings.Contains(out, "0644") {
		t.Errorf("expected a permission warning naming the mode:\n%s", out)
	}
}

// TestLogoutRevokesAndClears runs `logout` against a fake server: the stored
// token must be revoked server-side, then forgotten locally.
func TestLogoutRevokesAndClears(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/logout" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dcl_saved" {
			t.Errorf("logout sent %q", got)
		}
		revoked.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})

	cfg := Config{}
	cfg.UpsertAccount("me@example.com", s.URL, "dcl_saved")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	out := runRoot(t, []string{"logout"}, "")
	if !revoked.Load() {
		t.Error("logout never called the server")
	}
	if !strings.Contains(out, "me@example.com") {
		t.Errorf("logout did not name the account:\n%s", out)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "" {
		t.Errorf("logout left token %q in the config", got.Token)
	}
	if len(got.Accounts) != 0 {
		t.Errorf("logout left saved accounts behind: %+v", got.Accounts)
	}
}

// TestAccountSwitchSelectsSavedToken runs `account switch` and checks the saved
// token becomes the live session once the server confirms it.
func TestAccountSwitchSelectsSavedToken(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dcl_work" {
			t.Errorf("switch probed with %q", got)
		}
		_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "work@example.com"})
	})

	cfg := Config{}
	cfg.UpsertAccount("work@example.com", s.URL, "dcl_work")
	cfg.UpsertAccount("home@example.com", s.URL, "dcl_home")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"account", "switch", "work@example.com"}, "")

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_work" {
		t.Errorf("active token = %q, want dcl_work", got.Token)
	}
}

// TestAccountSwitchRejectsRevokedToken: a saved sign-in the server no longer
// accepts must be reported, and must not become the live session.
func TestAccountSwitchRejectsRevokedToken(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","code":"unauthorized"}`))
	})

	cfg := Config{}
	cfg.UpsertAccount("work@example.com", s.URL, "dcl_dead")
	cfg.UpsertAccount("home@example.com", s.URL, "dcl_home")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"account", "switch", "work@example.com"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected a stale-token error, output:\n%s", out.String())
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_home" {
		t.Errorf("a failed switch changed the session: %q", got.Token)
	}
}

// TestAccountListMarksActive: the table must name the live account and no
// other one.
func TestAccountListMarksActive(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	cfg := Config{}
	cfg.UpsertAccount("work@example.com", "https://a.example", "dcl_work")
	cfg.UpsertAccount("home@example.com", "https://b.example", "dcl_home")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	out := runRoot(t, []string{"account", "list"}, "")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	marks := map[string]string{}
	for _, ln := range lines[1:] { // line 0 is the header
		f := strings.Fields(ln)
		if len(f) < 3 {
			t.Fatalf("unexpected row %q", ln)
		}
		marks[f[0]] = f[len(f)-1]
	}
	if marks["home@example.com"] != "yes" {
		t.Errorf("live account not marked: %q", out)
	}
	if marks["work@example.com"] != "no" {
		t.Errorf("dormant account marked active: %q", out)
	}
}

// TestAccountListJSONHidesTokens pins the review finding: every saved entry
// carries a live full-privilege token, so `--json` must not emit it.
func TestAccountListJSONHidesTokens(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	cfg := Config{}
	cfg.UpsertAccount("work@example.com", "https://a.example", "dcl_secret_work")
	cfg.UpsertAccount("home@example.com", "https://b.example", "dcl_secret_home")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	out := runRoot(t, []string{"--json", "account", "list"}, "")
	if strings.Contains(out, "dcl_") {
		t.Errorf("account list --json leaked a credential:\n%s", out)
	}
	var views []map[string]any
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatalf("account list --json is not valid JSON: %v\n%s", err, out)
	}
	if len(views) != 2 {
		t.Fatalf("got %d entries, want 2:\n%s", len(views), out)
	}
	if views[1]["email"] != "home@example.com" || views[1]["active"] != true {
		t.Errorf("live entry wrong: %+v", views[1])
	}
	if views[0]["active"] != false {
		t.Errorf("dormant entry marked active: %+v", views[0])
	}
}

// TestLoginJSONIsOnlyJSON: with --json stdout is the response object and
// nothing else — the legal notice and the human lines must be suppressed.
func TestLoginJSONIsOnlyJSON(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/verify" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
	})

	out := runRoot(t, []string{"--server", s.URL, "--json", "login", "--email", "me@example.com", "--code", "123456"}, "")
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("login --json is not valid JSON: %v\n%s", err, out)
	}
	if got["token"] != "dcl_new" {
		t.Errorf("token = %q, want dcl_new", got["token"])
	}
}

// TestLogoutJSONWithNoSession: an already-signed-out machine still answers in
// the machine-readable shape — and needs no server to say so.
func TestLogoutJSONWithNoSession(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	out := runRoot(t, []string{"--json", "logout"}, "")
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("logout --json is not valid JSON: %v\n%s", err, out)
	}
	if got["status"] != "signed_out" {
		t.Errorf("status = %q, want signed_out", got["status"])
	}
}

// TestAccountListNeedsNoServer: listing saved sign-ins reads the config, so a
// machine that has never named an endpoint must not be asked for one.
func TestAccountListNeedsNoServer(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	out := runRoot(t, []string{"account", "list"}, "")
	if !strings.Contains(out, "No saved accounts") {
		t.Errorf("account list on a fresh machine:\n%s", out)
	}
}

// TestLogoutWithExplicitTokenLeavesConfig pins the precedence rule: a
// credential named by --token is the caller's, so revoking it must not rewrite
// the saved sign-in even when the values happen to be equal.
func TestLogoutWithExplicitTokenLeavesConfig(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/logout" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		revoked.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := Config{}
	cfg.UpsertAccount("me@example.com", s.URL, "dcl_saved")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--token", "dcl_saved", "logout"}, "")

	if !revoked.Load() {
		t.Error("logout did not revoke the explicit token")
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_saved" || len(got.Accounts) != 1 {
		t.Errorf("an explicit token rewrote the saved session: %+v", got)
	}
}

// TestLaxConfigPermsWarnsOnDormantAccount: `accounts` holds tokens too, so a
// config whose live session was signed out is still a credential file.
func TestLaxConfigPermsWarnsOnDormantAccount(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	cfg := Config{
		ServerURL: "https://stored.example",
		Accounts:  []Account{{Email: "me@example.com", ServerURL: "https://stored.example", Token: "dcl_dormant"}},
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRoot(t, []string{"account", "list"}, "")
	if !strings.Contains(out, "0644") {
		t.Errorf("expected a permission warning naming the mode:\n%s", out)
	}
}

// TestReloginRevokesSupersededToken: signing in again on this machine replaces
// its token for that email, so the superseded one must not stay live on the
// server with nothing left pointing at it.
func TestReloginRevokesSupersededToken(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			if got := r.Header.Get("Authorization"); got != "Bearer dcl_old" {
				t.Errorf("revoked %q, want the superseded token", got)
			}
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cfg := Config{}
	cfg.UpsertAccount("me@example.com", s.URL, "dcl_old")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")

	if !revoked.Load() {
		t.Error("the superseded token was never revoked")
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_new" {
		t.Errorf("live token = %q, want dcl_new", got.Token)
	}
	if len(got.Accounts) != 1 {
		t.Errorf("re-login stacked instead of replacing: %+v", got.Accounts)
	}
}

// TestReloginPersistsBeforeRevoking pins the ordering: when a re-login
// supersedes this machine's token, the replacement must already be on disk
// before the old one is revoked. Revoking first would leave the machine with
// no working credential at all if the write failed.
func TestReloginPersistsBeforeRevoking(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			// The handler runs at revoke time: whatever is on disk right now
			// is what survives if the process dies next.
			onDisk, err := Load()
			if err != nil {
				t.Errorf("config unreadable at revoke time: %v", err)
			}
			if onDisk.Token != "dcl_new" {
				t.Errorf("revoked the superseded token before storing the new one: on disk = %q", onDisk.Token)
			}
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cfg := Config{}
	cfg.UpsertAccount("me@example.com", s.URL, "dcl_old")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")

	if !revoked.Load() {
		t.Error("the superseded token was never revoked")
	}
}

// TestEmptyTokenFileKeepsSavedSession: an explicit --token-file that happens to
// be empty resolves to the config token, but the caller named a source — so
// logout must not rewrite a session the file never supplied.
func TestEmptyTokenFileKeepsSavedSession(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/logout" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		revoked.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := Config{}
	cfg.UpsertAccount("me@example.com", s.URL, "dcl_saved")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty-token")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "domain-cli", "config.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--token-file", empty, "logout"}, "")

	if !revoked.Load() {
		t.Error("logout did not revoke the resolved credential")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("an explicit --token-file rewrote the saved session:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestLoginOnSecondServer: an existing session must not block signing in to a
// different server, and the stored credential must not travel there.
func TestLoginOnSecondServer(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	stored := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the stored session's server was contacted: %s %s", r.Method, r.URL.Path)
	})
	var gotAuth string
	fresh := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/auth/verify" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_fresh"})
	})
	cfg := Config{}
	cfg.UpsertAccount("me@example.com", stored.URL, "dcl_stored")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--server", fresh.URL, "login", "--email", "second@example.com", "--code", "123456"}, "")

	if gotAuth != "" {
		t.Errorf("the stored credential was sent to the new server: %q", gotAuth)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_fresh" || !sameServer(got.ServerURL, fresh.URL) {
		t.Errorf("the new sign-in is not the live session: %+v", got)
	}
	if len(got.Accounts) != 2 {
		t.Errorf("expected both accounts saved, got %+v", got.Accounts)
	}
}

// TestAccountCommandJSONIsOnlyJSON: under --json these commands must answer in
// the machine-readable shape, as account list and logout do.
func TestAccountCommandJSONIsOnlyJSON(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "work@example.com"})
		case "/v1/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cfg := Config{}
	cfg.UpsertAccount("work@example.com", s.URL, "dcl_work")
	cfg.UpsertAccount("home@example.com", s.URL, "dcl_home")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"switch", []string{"--json", "account", "switch", "work@example.com"}, "switched"},
		{"remove", []string{"--json", "account", "remove", "home@example.com"}, "removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := runRoot(t, tc.args, "")
			var got map[string]string
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("--json output is not valid JSON: %v\n%s", err, out)
			}
			if got["status"] != tc.want {
				t.Errorf("status = %q, want %q", got["status"], tc.want)
			}
		})
	}
}

// TestMalformedConfigIsNotClobbered: a config that fails to parse must abort the
// command rather than be treated as empty — saving the zero value would drop
// every other saved sign-in.
func TestMalformedConfigIsNotClobbered(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	configPath := filepath.Join(dir, "domain-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := []byte("{\"server_url\": \"https://a.example\", \"accounts\": [")
	if err := os.WriteFile(configPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/verify" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
	})

	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the unreadable config to abort the command, output:\n%s", out.String())
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(broken) {
		t.Errorf("the unparseable config was overwritten:\nbefore: %s\nafter:  %s", broken, after)
	}
}

// TestAccountCommandsIgnoreUnrelatedServer: account list, switch and remove
// never send the root credential — they read the config or build their own
// client — so naming a different --server must not be rejected.
func TestAccountCommandsIgnoreUnrelatedServer(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var probed atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			probed.Store(true)
			_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "work@example.com"})
		case "/v1/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cfg := Config{}
	cfg.UpsertAccount("work@example.com", s.URL, "dcl_work")
	cfg.UpsertAccount("home@example.com", s.URL, "dcl_home")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	// Plain http on a non-loopback host: ValidateServerURL rejects it, so the
	// command only passes if it never validates a server it does not call.
	const other = "http://other.example"
	out := runRoot(t, []string{"--server", other, "account", "list"}, "")
	marks := map[string]string{}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, ln := range lines[1:] { // line 0 is the header
		f := strings.Fields(ln)
		if len(f) < 3 {
			t.Fatalf("unexpected row %q", ln)
		}
		marks[f[0]] = f[len(f)-1]
	}
	if marks["home@example.com"] != "yes" {
		t.Errorf("an unrelated --server blanked the live marker:\n%s", out)
	}
	if marks["work@example.com"] != "no" {
		t.Errorf("a dormant account was marked active:\n%s", out)
	}
	runRoot(t, []string{"--server", other, "account", "switch", "work@example.com"}, "")
	if !probed.Load() {
		t.Error("account switch did not probe the selected entry's own server")
	}
	runRoot(t, []string{"--server", other, "account", "remove", "home@example.com"}, "")
}

// TestLogoutFailsOnUnreadableConfig: "no token" and "the store could not be
// read" look identical from the resolved session. Claiming a clean sign-out for
// the second would leave a live credential on disk while telling the caller
// there was nothing to do.
func TestLogoutFailsOnUnreadableConfig(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	configPath := filepath.Join(dir, "domain-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := []byte("{\"server_url\": \"https://a.example\", \"token\": \"dcl_a\",")
	if err := os.WriteFile(configPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"logout"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the unreadable config to abort logout, output:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Not signed in") {
		t.Errorf("logout reported a clean sign-out for an unreadable store:\n%s", out.String())
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(broken) {
		t.Errorf("logout rewrote the unreadable config:\nbefore: %s\nafter:  %s", broken, after)
	}
}

// TestAccountListNeedsNoServerWithToken: account list sends no credential, so a
// token from the environment must not drag in a --server requirement.
func TestAccountListNeedsNoServerWithToken(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "dcl_env")
	out := runRoot(t, []string{"account", "list"}, "")
	if !strings.Contains(out, "No saved accounts") {
		t.Errorf("account list with an env token but no server:\n%s", out)
	}
}

// TestWhoamiJSONIsOnlyJSON: --json writes the response object and nothing else.
func TestWhoamiJSONIsOnlyJSON(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "me@example.com"})
	})
	if err := Save(Config{ServerURL: s.URL, Token: "dcl_x"}); err != nil {
		t.Fatal(err)
	}

	out := runRoot(t, []string{"--json", "whoami"}, "")
	var me apitypes.Me
	if err := json.Unmarshal([]byte(out), &me); err != nil {
		t.Fatalf("whoami --json is not valid JSON: %v\n%s", err, out)
	}
	if me.Email != "me@example.com" {
		t.Errorf("email = %q, want me@example.com", me.Email)
	}
}

// TestAccountSwitchNeedsNoRootServer: switch resolves the selected entry's own
// server, so a config whose server_url is unset must still work.
func TestAccountSwitchNeedsNoRootServer(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "work@example.com"})
	})
	// No ServerURL at the top level: the entry carries its own.
	cfg := Config{Accounts: []Account{{Email: "work@example.com", ServerURL: s.URL, Token: "dcl_work"}}}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"account", "switch", "work@example.com"}, "")

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_work" || !sameServer(got.ServerURL, s.URL) {
		t.Errorf("switch did not select the entry: %+v", got)
	}
}

// TestUpgradeConfigRevokesReplacedSession: a config written before accounts
// existed keeps its session only in server_url/token. Signing in overwrites
// that record, so the old token must be revoked there or it stays live on the
// server forever with nothing pointing at it.
func TestUpgradeConfigRevokesReplacedSession(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			if got := r.Header.Get("Authorization"); got != "Bearer dcl_old" {
				t.Errorf("revoked %q, want the pre-accounts session token", got)
			}
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	// Exactly what the previous release wrote: no accounts key at all.
	if err := Save(Config{ServerURL: s.URL, Token: "dcl_old"}); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")

	if !revoked.Load() {
		t.Error("the pre-accounts live session was never revoked")
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "dcl_new" {
		t.Errorf("live token = %q, want dcl_new", got.Token)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Token != "dcl_new" {
		t.Errorf("the sign-in was not saved as an account: %+v", got.Accounts)
	}
}

// TestLogoutAbortsWhenStoreBecomesUnreadable: logout reads the store again on
// the token path. If that read fails it must abort rather than save the zero
// value over every saved sign-in.
func TestLogoutAbortsWhenStoreBecomesUnreadable(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	configPath := filepath.Join(dir, "domain-cli", "config.json")

	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/logout" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		// The store turns unreadable between the pre-run load and logout's own.
		if err := os.Chmod(configPath, 0o000); err != nil {
			t.Errorf("chmod: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := Config{}
	cfg.UpsertAccount("me@example.com", s.URL, "dcl_saved")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"logout"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the unreadable store to abort logout, output:\n%s", out.String())
	}

	if err := os.Chmod(configPath, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("logout rewrote the store it could not read:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestLoginJSONStepOneIsOnlyJSON: the code-request path answers in the same
// machine-readable shape, not with the human line.
func TestLoginJSONStepOneIsOnlyJSON(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/signup" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.SignupResponse{Status: "sent"})
	})

	out := runRoot(t, []string{"--server", s.URL, "--json", "login", "--email", "me@example.com"}, "")
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("login --json (code request) is not valid JSON: %v\n%s", err, out)
	}
	if got["status"] != "sent" || got["email"] != "me@example.com" {
		t.Errorf("got %+v", got)
	}
}

// TestLoginVerifiesOnlyWhenTheStoreIsReadable: Verify mints a live token on the
// server. If the store cannot be read that token would be orphaned — minted,
// never saved, never shown to the caller, impossible to revoke — so the read
// must happen first.
func TestLoginVerifiesOnlyWhenTheStoreIsReadable(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	configPath := filepath.Join(dir, "domain-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var verified atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/verify" {
			verified.Store(true)
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_orphan"})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})

	var out bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected the unreadable store to abort, output:\n%s", out.String())
	}
	if verified.Load() {
		t.Error("the server minted a token that could never be saved or shown")
	}
}

// TestLoginRevokesTokenWhenTheSaveFails: Verify has already minted a live token
// by the time the store is written. If that write fails the caller never sees
// the token, so it must be revoked rather than left live and unreachable.
func TestLoginRevokesTokenWhenTheSaveFails(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var revoked atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			breakConfigDir(t, dir)
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_stranded"})
		case "/v1/auth/logout":
			if got := r.Header.Get("Authorization"); got != "Bearer dcl_stranded" {
				t.Errorf("revoked %q, want the token the save could not store", got)
			}
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	runRootErr(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"})
	if !revoked.Load() {
		t.Error("a token the caller could never see was left live on the server")
	}
}

// TestLoginRevokesBothDisplacedCredentials: a config can hold an entry for this
// email *and* a live session no entry records (a hand-edited file). Signing in
// overwrites both, so both must be revoked.
func TestLoginRevokesBothDisplacedCredentials(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	// Counted, not merely recorded: a token revoked twice must not read the
	// same as one revoked once.
	var mu sync.Mutex
	revoked := map[string]int{}
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			mu.Lock()
			revoked[r.Header.Get("Authorization")]++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	// The entry for this email, plus a live session nothing records.
	if err := Save(Config{
		ServerURL: s.URL,
		Token:     "dcl_unrecorded",
		Accounts:  []Account{{Email: "me@example.com", ServerURL: s.URL, Token: "dcl_entry"}},
	}); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")

	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{"Bearer dcl_entry", "Bearer dcl_unrecorded"} {
		if revoked[want] != 1 {
			t.Errorf("%s revoked %d times, want 1; revoked = %v", want, revoked[want], revoked)
		}
	}
}

// TestLoginRevokesADisplacedTokenOnce: the entry for this email and the live
// session are independent but not disjoint — a hand-edited config can name the
// same token in both, under different server URLs. The token is displaced once,
// so it is revoked once, at the server its entry recorded: sending it to the
// other host would hand the credential to somewhere that never issued it.
func TestLoginRevokesADisplacedTokenOnce(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	var mu sync.Mutex
	revoked := map[string]int{}
	issuer := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			mu.Lock()
			revoked[r.Header.Get("Authorization")]++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	// A host the token was never minted for. It must see no request at all.
	stranger := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("sent %s %s with %q to a server that never issued the token",
			r.Method, r.URL.Path, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	})
	// One token, reachable two ways: the entry records the issuer, the live
	// session claims the stranger.
	if err := Save(Config{
		ServerURL: stranger.URL,
		Token:     "dcl_shared",
		Accounts:  []Account{{Email: "me@example.com", ServerURL: issuer.URL, Token: "dcl_shared"}},
	}); err != nil {
		t.Fatal(err)
	}

	runRoot(t, []string{"--server", issuer.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")

	mu.Lock()
	defer mu.Unlock()
	if got := revoked["Bearer dcl_shared"]; got != 1 {
		t.Errorf("revoked the displaced token %d times, want 1; revoked = %v", got, revoked)
	}
}

// TestLoginSaysTheCodeIsSpentWhenTheSaveFails: the write error alone points the
// caller back at a code Verify has already consumed, so the obvious retry fails
// a second time. The error says the sign-in was discarded and the code spent.
func TestLoginSaysTheCodeIsSpentWhenTheSaveFails(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			breakConfigDir(t, dir)
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_stranded"})
		case "/v1/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	out := runRootErr(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"})
	if !strings.Contains(out, "the verification code is spent") {
		t.Errorf("the caller was not told the code is spent, output:\n%s", out)
	}
	// The revocation worked, so there is nothing to warn about.
	if strings.Contains(out, "is still live on") {
		t.Errorf("warned about a token that was revoked, output:\n%s", out)
	}
}

// TestLoginWarnsWhenTheStrandedTokenSurvives: the revocation is the only thing
// standing between a failed write and a live token nothing records. When it
// fails too, saying so is all that is left.
func TestLoginWarnsWhenTheStrandedTokenSurvives(t *testing.T) {
	dir := setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			breakConfigDir(t, dir)
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_stranded"})
		case "/v1/auth/logout":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	out := runRootErr(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"})
	if !strings.Contains(out, "is still live on "+s.URL) {
		t.Errorf("a token left live on the server went unreported, output:\n%s", out)
	}
}

// TestLoginWarnsWhenASupersededTokenSurvives: UpsertAccount has already
// replaced the entry that named the old token, so a failed revocation strands
// it exactly as a failed write strands a new one. The sign-in itself is
// durable, so this warns rather than fails.
func TestLoginWarnsWhenASupersededTokenSurvives(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	if err := Save(Config{
		ServerURL: s.URL,
		Token:     "dcl_old",
		Accounts:  []Account{{Email: "me@example.com", ServerURL: s.URL, Token: "dcl_old"}},
	}); err != nil {
		t.Fatal(err)
	}

	// runRoot fails the test on a non-nil error: the sign-in must still succeed.
	out := runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")
	if !strings.Contains(out, "is still live on "+s.URL) {
		t.Errorf("a superseded token left live on the server went unreported, output:\n%s", out)
	}
	// The new session is durable whatever the old token did.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "dcl_new" {
		t.Errorf("live token = %q, want the new one", cfg.Token)
	}
}

// TestLoginNeverSendsATokenToAnInvalidServer: a saved entry can name a
// server_url the CLI may not send a credential to. Revoking there would hand
// the token to whoever controls that host, so the sign-in must make no request
// at all — and say so, rather than pass the skipped revocation over in silence.
func TestLoginNeverSendsATokenToAnInvalidServer(t *testing.T) {
	var logouts atomic.Int32
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			logouts.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cases := []struct {
		name   string
		badURL string
	}{
		// Unreachable, so only the reported reason can prove the refusal came
		// from the guard rather than from a request that failed on its own.
		{"plain http on a host that is not loopback", "http://elsewhere.example"},
		// Reachable: this is the test server itself, rejected for the
		// credentials in the URL. Drop the guard and the logout lands on the
		// counter below, which is the regression this pins.
		{"credentials embedded in the URL", strings.Replace(s.URL, "http://", "http://user:pw@", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setConfigDir(t)
			setLocale(t, "en")
			t.Setenv("DOMAIN_CLI_TOKEN", "")
			logouts.Store(0)
			if err := Save(Config{
				ServerURL: s.URL,
				Accounts:  []Account{{Email: "me@example.com", ServerURL: tc.badURL, Token: "dcl_offsite"}},
			}); err != nil {
				t.Fatal(err)
			}

			out := runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")
			if n := logouts.Load(); n != 0 {
				t.Errorf("sent %d logout request(s); the displaced token must not travel to an invalid server", n)
			}
			if !strings.Contains(out, "is still live on "+tc.badURL) {
				t.Errorf("the skipped revocation went unreported, output:\n%s", out)
			}
			// The reason names the server policy, so the refusal happened
			// before any request was built, not after one failed.
			if !strings.Contains(out, "The server must be an https URL") {
				t.Errorf("the revocation was not refused by the server guard, output:\n%s", out)
			}
		})
	}
}

// TestLoginLocalizesARateLimitedRevocation: a 429 arrives as a
// RateLimitedError whose Error() is hard-coded English. The warning is a
// localized sentence, so the detail is normalized the way command errors are
// rather than pasted in as-is.
func TestLoginLocalizesARateLimitedRevocation(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "ca")
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/verify":
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_new"})
		case "/v1/auth/logout":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	if err := Save(Config{
		ServerURL: s.URL,
		Token:     "dcl_old",
		Accounts:  []Account{{Email: "me@example.com", ServerURL: s.URL, Token: "dcl_old"}},
	}); err != nil {
		t.Fatal(err)
	}

	out := runRoot(t, []string{"--server", s.URL, "login", "--email", "me@example.com", "--code", "123456"}, "")
	if !strings.Contains(out, "Límit de peticions assolit, torna-ho a provar en 7s.") {
		t.Errorf("the rate-limit detail was not localized, output:\n%s", out)
	}
	if strings.Contains(out, "rate limited, retry after") {
		t.Errorf("the hard-coded English detail reached a Catalan warning, output:\n%s", out)
	}
}
