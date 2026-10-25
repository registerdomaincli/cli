package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// The update check compares the running binary against the latest GitHub
// release after every successful command and prints one stderr line when a
// newer release exists or when the check itself just failed. The outcome is
// cached in the config directory because the unauthenticated GitHub API
// allows 60 requests per hour per IP: an uncached check in a script loop
// would rate-limit itself into a permanent failure warning.
//
// A notice only ever goes to stderr, so --json output stays parseable, and
// DOMAIN_CLI_NO_UPDATE_CHECK (any non-empty value) turns the whole thing
// off. Builds whose version is not a plain vMAJOR.MINOR.PATCH tag ("dev",
// "test") never check: there is no release line to compare them against.

const updateAPIURL = "https://api.github.com/repos/registerdomaincli/cli/releases/latest"

// noUpdateCheckEnv opts a machine out of the release check entirely.
const noUpdateCheckEnv = "DOMAIN_CLI_NO_UPDATE_CHECK"

// updateStateFile sits beside config.json. It carries no credential, so the
// config permission dance does not apply to it.
const updateStateFile = "update-check.json"

// updateCheckInterval bounds how often GitHub is asked for the latest tag.
const updateCheckInterval = 24 * time.Hour

// updateHTTP has a short timeout: the check runs after the command's own
// work, but it must never hang the process on a dead network.
var updateHTTP = &http.Client{Timeout: 5 * time.Second}

// updateLatestRelease and updateNow are indirected for tests.
var (
	updateLatestRelease = fetchLatestRelease
	updateNow           = time.Now
)

// updateState is the cached outcome of the last check. Failed marks an
// attempt that could not reach GitHub; Latest is then empty and meaningless.
type updateState struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest,omitempty"`
	Failed    bool      `json:"failed,omitempty"`
}

func updateStatePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, updateStateFile), nil
}

// checkForUpdate returns the localized notice to print, or "" when the
// binary is current, the check does not apply, or the last attempt failed and
// that failure was already reported when it happened. A successful check
// keeps warning for the whole interval — "a new version exists" stays
// actionable until the next re-check — while "could not check" does not,
// or an offline machine would nag on every command.
func checkForUpdate(version string) string {
	if os.Getenv(noUpdateCheckEnv) != "" {
		return ""
	}
	local, ok := parseSemver(version)
	if !ok {
		return ""
	}
	path, err := updateStatePath()
	if err != nil {
		return ""
	}
	var st updateState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if updateNow().Sub(st.LastCheck) >= updateCheckInterval {
		latest, err := updateLatestRelease()
		st = updateState{LastCheck: updateNow(), Latest: latest, Failed: err != nil}
		if b, err := json.Marshal(st); err == nil {
			_ = os.MkdirAll(filepath.Dir(path), 0o700)
			_ = os.WriteFile(path, b, 0o600)
		}
		if err != nil {
			return i18n.T("update.checkFailed", "Err", err.Error())
		}
	} else if st.Failed {
		return ""
	}
	remote, ok := parseSemver(st.Latest)
	if !ok {
		return ""
	}
	if semverNewer(remote, local) {
		return i18n.T("update.available", "Latest", st.Latest, "Current", version)
	}
	return ""
}

// fetchLatestRelease asks GitHub for the tag of the latest published release.
// The request is unauthenticated: the repository is public, and a token would
// turn a version check into a credential handed to GitHub on every update
// attempt.
func fetchLatestRelease() (string, error) {
	req, err := http.NewRequest(http.MethodGet, updateAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updateHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// The repository is private or has no visible release: there is no
		// update line to compare against, which is "no information", not a
		// failed check — a CLI install behind a private repo must not nag
		// its user every day. Silent now, retried on the next interval.
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", errors.New("release response has no tag_name")
	}
	return release.TagName, nil
}

// parseSemver parses a "vMAJOR.MINOR.PATCH" release tag, ignoring any
// prerelease or build suffix. It returns false for anything else — "dev",
// "test", a sha-pinned build — so those versions never warn.
func parseSemver(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// semverNewer reports whether release a is strictly newer than b.
func semverNewer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// notifyUpdate prints the update notice on stderr. It runs from the root's
// PersistentPostRunE, so only successful commands pay for it — a failed
// command already has its error to report — and cobra skips it entirely for
// --version and --help. Commands whose output is consumed mechanically stay
// silent too: `help` is often scripted, and completion feeds a shell
// function that must not receive stray lines.
func notifyUpdate(cmd *cobra.Command, version string) {
	switch cmd.Name() {
	case "help", "completion", "__complete", "__completeNoDesc":
		return
	}
	if msg := checkForUpdate(version); msg != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
	}
}
