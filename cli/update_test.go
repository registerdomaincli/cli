package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The update check must never reach the real GitHub API from a test, so every
// case seeds the cache file first and stubs updateLatestRelease where a live
// fetch is actually part of the scenario.

func setUpdateEnv(t *testing.T) {
	t.Helper()
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv(noUpdateCheckEnv, "")
	t.Cleanup(func() {
		updateLatestRelease = fetchLatestRelease
		updateNow = time.Now
	})
}

func seedUpdateState(t *testing.T, st updateState) {
	t.Helper()
	path, err := updateStatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateWarnsOnNewerRelease(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour), Latest: "v9.9.9"})
	var fetched atomic.Bool
	updateLatestRelease = func() (string, error) {
		fetched.Store(true)
		return "v9.9.9", nil
	}
	if got := checkForUpdate("v1.0.0"); !strings.Contains(got, "v9.9.9") || !strings.Contains(got, "v1.0.0") {
		t.Fatalf("expected update notice naming both versions, got %q", got)
	}
	if !fetched.Load() {
		t.Fatal("expected a live fetch after the interval elapsed")
	}
}

func TestUpdateSilentWhenCurrent(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour), Latest: "v1.0.0"})
	updateLatestRelease = func() (string, error) { return "v1.0.0", nil }
	if got := checkForUpdate("v1.0.0"); got != "" {
		t.Fatalf("expected silence when current, got %q", got)
	}
}

func TestUpdateOlderLatestSilent(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour), Latest: "v0.9.0"})
	updateLatestRelease = func() (string, error) { return "v0.9.0", nil }
	if got := checkForUpdate("v1.2.3"); got != "" {
		t.Fatalf("expected silence when installed is newer than latest release, got %q", got)
	}
}

func TestUpdateFetchFailureWarnsAndCaches(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour)})
	updateLatestRelease = func() (string, error) { return "", errors.New("dial tcp: refused") }
	if got := checkForUpdate("v1.0.0"); !strings.Contains(got, "dial tcp") {
		t.Fatalf("expected failure notice carrying the cause, got %q", got)
	}
	// The failure is cached: within the interval a repeat call stays silent,
	// so an offline machine is not nagged on every command.
	updateLatestRelease = func() (string, error) { return "v9.9.9", nil }
	if got := checkForUpdate("v1.0.0"); got != "" {
		t.Fatalf("expected cached failure to suppress repeat warnings, got %q", got)
	}
}

func TestUpdateCachedFreshStateNoFetch(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now(), Latest: "v9.9.9"})
	updateLatestRelease = func() (string, error) {
		t.Fatal("must not fetch while the cached check is fresh")
		return "", nil
	}
	if got := checkForUpdate("v1.0.0"); !strings.Contains(got, "v9.9.9") {
		t.Fatalf("expected notice served from cache, got %q", got)
	}
}

func TestUpdateDisabledByEnv(t *testing.T) {
	setUpdateEnv(t)
	t.Setenv(noUpdateCheckEnv, "1")
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour), Latest: "v9.9.9"})
	updateLatestRelease = func() (string, error) { t.Fatal("must not fetch"); return "", nil }
	if got := checkForUpdate("v1.0.0"); got != "" {
		t.Fatalf("expected env opt-out to silence the check, got %q", got)
	}
}

func TestUpdateDevVersionNeverChecks(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour), Latest: "v9.9.9"})
	updateLatestRelease = func() (string, error) { t.Fatal("must not fetch"); return "", nil }
	if got := checkForUpdate("dev"); got != "" {
		t.Fatalf("expected dev builds to skip the check, got %q", got)
	}
}

func TestUpdateParseSemver(t *testing.T) {
	cases := []struct {
		in     string
		want   [3]int
		wantOK bool
	}{
		{"v1.2.3", [3]int{1, 2, 3}, true},
		{"1.2.3", [3]int{1, 2, 3}, true},
		{"v1.2.3-rc.1", [3]int{1, 2, 3}, true},
		{"v1.2", [3]int{1, 2, 0}, true},
		{"v1", [3]int{1, 0, 0}, true},
		{"dev", [3]int{}, false},
		{"test", [3]int{}, false},
		{"", [3]int{}, false},
		{"v1.2.3.4", [3]int{}, false},
		{"vX.Y.Z", [3]int{}, false},
	}
	for _, c := range cases {
		got, ok := parseSemver(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("parseSemver(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestUpdateSemverNewer(t *testing.T) {
	cases := []struct {
		a, b [3]int
		want bool
	}{
		{[3]int{2, 0, 0}, [3]int{1, 9, 9}, true},
		{[3]int{1, 1, 0}, [3]int{1, 0, 9}, true},
		{[3]int{1, 0, 1}, [3]int{1, 0, 0}, true},
		{[3]int{1, 0, 0}, [3]int{1, 0, 0}, false},
		{[3]int{1, 0, 0}, [3]int{2, 0, 0}, false},
	}
	for _, c := range cases {
		if got := semverNewer(c.a, c.b); got != c.want {
			t.Errorf("semverNewer(%v, %v) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestUpdatePostRunWiring runs a real command through the root and asserts the
// notice lands on stderr (merged here) and that --version does not trigger it.
func TestUpdatePostRunWiring(t *testing.T) {
	setUpdateEnv(t)
	seedUpdateState(t, updateState{LastCheck: time.Now().Add(-25 * time.Hour), Latest: "v9.9.9"})
	updateLatestRelease = func() (string, error) { return "v9.9.9", nil }
	t.Setenv("SSH_CONNECTION", "1")

	var out strings.Builder
	root := NewRootCmd("v1.0.0")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"account", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute account list: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "v9.9.9") {
		t.Fatalf("expected update notice after a successful command, got:\n%s", out.String())
	}

	// --version is handled by cobra without running PostRun; the check must
	// not have run here either.
	out.Reset()
	root2 := NewRootCmd("v1.0.0")
	root2.SetOut(&out)
	root2.SetErr(&out)
	root2.SetArgs([]string{"--version"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if strings.Contains(out.String(), "update") || strings.Contains(out.String(), "v9.9.9") {
		t.Fatalf("--version must not print an update notice, got:\n%s", out.String())
	}
}

// TestUpdateFetchLatestReleaseShape pins the request/response contract with
// GitHub without leaving the machine.
func TestUpdateFetchLatestReleaseShape(t *testing.T) {
	old := updateHTTP
	t.Cleanup(func() { updateHTTP = old })
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing Accept header")
		}
		if r.URL.Path != "/repos/registerdomaincli/cli/releases/latest" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3"})
	}))
	defer s.Close()
	// Point the client at the test server by stubbing the transport.
	updateHTTP = &http.Client{Timeout: 5 * time.Second, Transport: redirectTransport{to: s.URL}}
	tag, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("tag = %q, want v1.2.3", tag)
	}
}

// TestUpdate404MeansNoInformation pins the private-repo behavior: GitHub
// answers 404 for the releases endpoint when the repo is private, and that is
// "no update line available", not a failed check — the CLI stays silent and
// retries on the next interval instead of nagging daily.
func TestUpdate404MeansNoInformation(t *testing.T) {
	old := updateHTTP
	t.Cleanup(func() { updateHTTP = old })
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer s.Close()
	updateHTTP = &http.Client{Timeout: 5 * time.Second, Transport: redirectTransport{to: s.URL}}
	tag, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("404 must not surface as a failed check, got %v", err)
	}
	if tag != "" {
		t.Fatalf("404 must yield no tag, got %q", tag)
	}
}

type redirectTransport struct{ to string }

func (r redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme, req2.URL.Host = "http", strings.TrimPrefix(r.to, "http://")
	return (&http.Client{}).Do(req2)
}

// TestUpdateHTTPFailureSurfaces feeds a dead endpoint through the real fetch
// path so the error text reaches the localized notice.
func TestUpdateHTTPFailureSurfaces(t *testing.T) {
	old := updateHTTP
	t.Cleanup(func() { updateHTTP = old })
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer s.Close()
	updateHTTP = &http.Client{Timeout: 5 * time.Second, Transport: redirectTransport{to: s.URL}}
	_, err := fetchLatestRelease()
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected status error, got %v", err)
	}
}
