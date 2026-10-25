package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// setConfigDir points the config helpers at a throwaway directory.
func setConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userConfigDir = old })
	return dir
}

func TestConfigRoundTrip(t *testing.T) {
	dir := setConfigDir(t)
	cfg := Config{ServerURL: "https://registerdomaincli.com", Token: "dcl_abc"}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("round trip mismatch: %+v != %+v", got, cfg)
	}
	// The token is a full-privilege credential: file 0600, dir 0700.
	path := filepath.Join(dir, "domain-cli", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config.json mode = %o, want 600", info.Mode().Perm())
	}
	dinfo, err := os.Stat(filepath.Join(dir, "domain-cli"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dinfo.Mode().Perm() != 0o700 {
		t.Errorf("config dir mode = %o, want 700", dinfo.Mode().Perm())
	}
}

func TestLoadMissingFile(t *testing.T) {
	setConfigDir(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load on first run should not error: %v", err)
	}
	if !reflect.DeepEqual(cfg, Config{}) {
		t.Errorf("expected zero config, got %+v", cfg)
	}
}

func TestResolveServerPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		flag   string
		env    string
		config string
		want   string
	}{
		{"flag wins", "http://flag", "http://env", "http://config", "http://flag"},
		{"env beats config", "", "http://env", "http://config", "http://env"},
		{"config used when others empty", "", "", "http://config", "http://config"},
		{"all empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DOMAIN_CLI_SERVER_URL", tt.env)
			got := ResolveServer(tt.flag, Config{ServerURL: tt.config})
			if got != tt.want {
				t.Errorf("ResolveServer = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTokenPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		flag   string
		env    string
		config string
		want   string
	}{
		{"flag wins", "dcl_flag", "dcl_env", "dcl_config", "dcl_flag"},
		{"env beats config", "", "dcl_env", "dcl_config", "dcl_env"},
		{"config used when others empty", "", "", "dcl_config", "dcl_config"},
		{"all empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DOMAIN_CLI_TOKEN", tt.env)
			got := ResolveToken(tt.flag, Config{Token: tt.config})
			if got != tt.want {
				t.Errorf("ResolveToken = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultServerURL(t *testing.T) {
	t.Setenv("DOMAIN_CLI_ENV", "dev")
	if got := DefaultServerURL(); got != "http://localhost:8080" {
		t.Errorf("dev default = %q", got)
	}
	t.Setenv("DOMAIN_CLI_ENV", "")
	if got := DefaultServerURL(); got != "https://registerdomaincli.com" {
		t.Errorf("production default = %q", got)
	}
}

// TestSaveForcesSecurePermissions pins the fix for a config that already
// exists with lax modes: os.WriteFile applies its perm argument only when it
// creates the file and MkdirAll leaves an existing directory alone, so the
// token used to stay readable by others.
func TestSaveForcesSecurePermissions(t *testing.T) {
	setConfigDir(t)
	dir, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{Token: "dcl_secret"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("config file mode = %04o, want 0600", got)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("config dir mode = %04o, want 0700", got)
	}
}

// TestSaveReplacesSymlink checks a pre-planted config.json symlink cannot
// redirect the token into a file the attacker chose.
func TestSaveReplacesSymlink(t *testing.T) {
	setConfigDir(t)
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{Token: "dcl_secret"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "untouched" {
		t.Errorf("wrote through the symlink: target now %q", got)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("config.json is still a symlink")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Token != "dcl_secret" {
		t.Errorf("token = %q, want the saved one", cfg.Token)
	}
}

// TestTokenBelongsToServer pins the binding rule: a token read from the config
// file belongs to the server recorded beside it, and only an explicit token
// overrides that.
func TestTokenBelongsToServer(t *testing.T) {
	t.Setenv("DOMAIN_CLI_TOKEN", "")
	tests := []struct {
		name          string
		explicitToken string
		storedToken   string
		server        string
		want          bool
	}{
		{"same server", "", "dcl_a", "https://a.example", true},
		{"trailing slash", "", "dcl_a", "https://a.example/", true},
		{"other server", "", "dcl_a", "https://b.example", false},
		{"explicit token overrides", "dcl_b", "dcl_a", "https://b.example", true},
		{"nothing stored", "", "", "https://b.example", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{ServerURL: "https://a.example", Token: tc.storedToken}
			if got := tokenBelongsToServer(tc.explicitToken, cfg, tc.server); got != tc.want {
				t.Errorf("tokenBelongsToServer(%q, token=%q, %q) = %v, want %v",
					tc.explicitToken, tc.storedToken, tc.server, got, tc.want)
			}
		})
	}
}

// TestAccountStore pins the multi-account invariants: the live session is the
// account `account switch` selected, re-signing in replaces rather than stacks,
// and forgetting the live account takes its token with it so no credential
// outlives the entry that named it.
func TestAccountStore(t *testing.T) {
	setConfigDir(t)
	var cfg Config
	cfg.UpsertAccount("work@example.com", "https://a.example", "dcl_work")
	cfg.UpsertAccount("home@example.com", "https://b.example", "dcl_home")

	if cfg.Token != "dcl_home" || cfg.ServerURL != "https://b.example" {
		t.Fatalf("the last sign-in is not the live session: %+v", cfg)
	}
	if a, ok := cfg.ActiveAccount(); !ok || a.Email != "home@example.com" {
		t.Errorf("ActiveAccount = %+v, %v", a, ok)
	}
	// A human types what the server lower-cased.
	if a, ok := cfg.FindAccount("HOME@example.com"); !ok || a.Token != "dcl_home" {
		t.Errorf("FindAccount = %+v, %v", a, ok)
	}

	if !cfg.SetActive("work@example.com") || cfg.Token != "dcl_work" {
		t.Fatalf("SetActive did not select the saved token: %+v", cfg)
	}
	if cfg.SetActive("nobody@example.com") {
		t.Error("SetActive accepted an unknown email")
	}

	// Re-signing in replaces the entry instead of stacking a second one.
	cfg.UpsertAccount("work@example.com", "https://a.example", "dcl_work2")
	if len(cfg.Accounts) != 2 {
		t.Fatalf("re-login stacked a duplicate: %+v", cfg.Accounts)
	}

	// Removing an unknown email leaves the live session alone.
	if cfg.RemoveAccount("nobody@example.com") {
		t.Error("RemoveAccount reported removing an unknown email")
	}
	if cfg.Token != "dcl_work2" {
		t.Errorf("a no-op removal disturbed the session: %q", cfg.Token)
	}

	// Removing the live account clears its token and keeps the server URL.
	if !cfg.RemoveAccount("work@example.com") {
		t.Fatal("RemoveAccount reported no removal")
	}
	if cfg.Token != "" {
		t.Errorf("removing the live account left token %q", cfg.Token)
	}
	if cfg.ServerURL != "https://a.example" {
		t.Errorf("removing the live account dropped the server: %q", cfg.ServerURL)
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("round trip mismatch: %+v != %+v", got, cfg)
	}
}
