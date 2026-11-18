// Command pluginbundle builds the Claude directory plugin under plugin/ from
// the repository's canonical sources, so the plugin never drifts from them:
//
//   - plugin/skills/domain-cli/SKILL.md: skills/domain-cli/SKILL.md with the
//     plugin edits below applied (the web keeps serving the canonical file);
//   - plugin/skills/domain-cli/scripts/install.sh: a copy of install.sh, run
//     by the plugin skill instead of `curl … | sh` (directory policy: a plugin
//     runs scripts it ships, not ones it downloads);
//   - plugin/LICENSE: a copy of LICENSE.
//
// Run `go run ./tools/pluginbundle` after editing any source; CI runs it with
// -check and fails when plugin/ is stale. Every edit must match its source
// text exactly once, so rewording the canonical skill fails loudly here
// instead of silently shipping the unedited text in the plugin.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type edit struct{ why, old, new string }

var skillEdits = []edit{
	{
		why: "transfer quotes go through the connector (no raw API call needing the CLI token)",
		old: "  `discount_code`) or\n" +
			"  `GET /v1/quote?domain=…&years=1&kind=transfer&currency=<code>&discount=<code>`\n" +
			"  (see the API section). Pin the currency",
		new: "  `discount_code`). Pin the currency",
	},
	{
		why: "structured reads go through the connector tools",
		old: "For structured reads use the\n" +
			"  read-only API calls below (`/v1/domains/search`, `/v1/domains`,\n" +
			"  `/v1/domains/<domain>/records`, `/v1/billing`, …) or the matching MCP\n" +
			"  tools.\n",
		new: "For structured reads use the\n" +
			"  read-only tools of the MCP connector bundled with this plugin\n" +
			"  (`search_domains`, `list_domains`, `list_records`, `get_billing`, …).\n",
	},
	{
		why: "run the installer bundled with the plugin, not a downloaded script",
		old: "- Linux/macOS: `curl -fsSL https://registerdomaincli.com/install.sh | sh`\n",
		new: "- Linux/macOS: `sh \"${CLAUDE_SKILL_DIR}/scripts/install.sh\"`, the\n" +
			"  installer bundled with this skill (the same script registerdomaincli.com\n" +
			"  serves). It downloads the release binary from GitHub and verifies its\n" +
			"  SHA-256 before installing it.\n",
	},
}

// The canonical skill's "Direct API access" section reads the CLI's saved
// token from its config file to call the API with curl. A plugin must not
// read credentials from the user's machine, and it does not need to: its MCP
// connector signs in with OAuth and the CLI uses its own session.
const (
	apiSectionStart = "## Direct API access\n"
	apiSectionEnd   = "## Errors and rate limits\n"
	apiSectionNew   = "## Direct API access\n\n" +
		"Use the MCP connector bundled with this plugin, or the CLI: between them\n" +
		"they cover the whole API. Never read the CLI's saved token (its\n" +
		"`config.json`) or any other credential from the user's machine to call\n" +
		"the API yourself; the connector signs in through OAuth in the human's\n" +
		"browser, and the CLI uses its own saved session.\n\n" +
		"Signing out is `domain-cli logout`, only when the human asks: it revokes\n" +
		"this machine's token on the server and clears it from the config.\n\n"
)

func pluginSkill(src string) (string, error) {
	for _, e := range skillEdits {
		if n := strings.Count(src, e.old); n != 1 {
			return "", fmt.Errorf("skill edit %q: expected 1 match, found %d", e.why, n)
		}
		src = strings.Replace(src, e.old, e.new, 1)
	}
	i := strings.Index(src, apiSectionStart)
	j := strings.Index(src, apiSectionEnd)
	if i < 0 || j < i || strings.Count(src, apiSectionStart) != 1 {
		return "", fmt.Errorf("skill: cannot find the %q … %q section", strings.TrimSpace(apiSectionStart), strings.TrimSpace(apiSectionEnd))
	}
	return src[:i] + apiSectionNew + src[j:], nil
}

type output struct {
	path string
	data []byte
	mode os.FileMode
}

func build(root string) ([]output, error) {
	read := func(p string) ([]byte, error) { return os.ReadFile(filepath.Join(root, p)) }
	skill, err := read("skills/domain-cli/SKILL.md")
	if err != nil {
		return nil, err
	}
	ps, err := pluginSkill(string(skill))
	if err != nil {
		return nil, err
	}
	install, err := read("install.sh")
	if err != nil {
		return nil, err
	}
	license, err := read("LICENSE")
	if err != nil {
		return nil, err
	}
	return []output{
		{"plugin/skills/domain-cli/SKILL.md", []byte(ps), 0o644},
		{"plugin/skills/domain-cli/scripts/install.sh", install, 0o755},
		{"plugin/LICENSE", license, 0o644},
	}, nil
}

func main() {
	check := flag.Bool("check", false, "verify plugin/ is up to date instead of writing it")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	outs, err := build(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pluginbundle:", err)
		os.Exit(1)
	}
	stale := false
	for _, o := range outs {
		p := filepath.Join(*root, o.path)
		if *check {
			got, err := os.ReadFile(p)
			if err != nil || !bytes.Equal(got, o.data) {
				fmt.Fprintf(os.Stderr, "pluginbundle: %s is stale; run: go run ./tools/pluginbundle\n", o.path)
				stale = true
				continue
			}
			if st, err := os.Stat(p); err == nil && st.Mode().Perm() != o.mode {
				fmt.Fprintf(os.Stderr, "pluginbundle: %s has mode %v, want %v\n", o.path, st.Mode().Perm(), o.mode)
				stale = true
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "pluginbundle:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(p, o.data, o.mode); err != nil {
			fmt.Fprintln(os.Stderr, "pluginbundle:", err)
			os.Exit(1)
		}
		if err := os.Chmod(p, o.mode); err != nil {
			fmt.Fprintln(os.Stderr, "pluginbundle:", err)
			os.Exit(1)
		}
	}
	if stale {
		os.Exit(1)
	}
}
