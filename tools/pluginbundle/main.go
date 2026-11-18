// Command pluginbundle builds the Claude directory plugin under plugin/ from
// the repository's canonical sources, so the plugin never drifts from them:
//
//   - plugin/skills/domain-cli/SKILL.md: skills/domain-cli/SKILL.md with the
//     plugin edits below applied (the web keeps serving the canonical file);
//   - plugin/LICENSE: a copy of LICENSE.
//
// Directory policy shapes the edits: a plugin from an organization that is
// not Verified may not ship scripts or programs, must not download and run
// installers, and must not read credentials from the user's machine. So the
// plugin skill never installs the CLI itself: the plugin's bundled MCP
// connector covers every operation, and a human who wants the CLI installs
// it from the website.
//
// Run `go run ./tools/pluginbundle` after editing any source; CI runs it with
// -check and fails when plugin/ is stale or holds files nothing generates
// under plugin/skills. Every edit must match its source text exactly once,
// so rewording the canonical skill fails loudly here instead of silently
// shipping the unedited text in the plugin.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type edit struct{ why, old, new string }

var skillEdits = []edit{
	{
		why: "no CLI on the machine: use the bundled connector, never install",
		old: "- **Missing pieces are your job too.** No `domain-cli` on the machine →\n" +
			"  install it (see **Install**), telling the human first. No account → run\n" +
			"  `signup` and ask them only for the emailed 6-digit code.",
		new: "- **Missing pieces are your job too.** No `domain-cli` on the machine →\n" +
			"  use the MCP connector bundled with this plugin (see **Install**). No\n" +
			"  account → the connector's sign-in creates it; the human only types the\n" +
			"  emailed 6-digit code.",
	},
	{
		why: "the plugin's way in is its connector, not a CLI it installs",
		old: "- **Have a shell? Use the CLI, and install it yourself.** That is why\n" +
			"  RegisterDomainCLI ships a CLI and not only an MCP server: installing\n" +
			"  `domain-cli` is one command you can run on your own once the human says\n" +
			"  yes, with no configuration and no restart. Adding an MCP server usually\n" +
			"  has to be done by hand by the human, and even when you can edit the\n" +
			"  config yourself the harness normally needs a restart before the tools\n" +
			"  show up. So never send a human with a shell off to set up the MCP\n" +
			"  connector. Install the CLI instead. (If the connector is already\n" +
			"  connected in this session, using it is fine.)\n",
		new: "- **Use the connector bundled with this plugin.** It is already\n" +
			"  configured, and its tools cover search, quotes, orders, DNS,\n" +
			"  nameservers, contacts and billing. If `domain-cli` is already\n" +
			"  installed and signed in you may use it as well, but never install\n" +
			"  software from this plugin.\n",
	},
	{
		why: "in the plugin the connector is already configured, shell or not",
		old: "same account, same dcl_ token, same API. This path is for agents WITHOUT a\n" +
			"shell: with a shell, install the CLI (see above) rather than asking the\n" +
			"human to configure a connector and restart.\n",
		new: "same account, same dcl_ token, same API. This plugin bundles it already\n" +
			"configured, so use it whether or not there is a shell.\n",
	},
	{
		why: "a stale CLI is the human's to update; the plugin installs nothing",
		old: "  A release newer than the printed `vX.Y.Z` means the install is behind:\n" +
			"  run the install command from **Install** again (it is idempotent), or\n" +
			"  `go install …@latest` for source installs.",
		new: "  A release newer than the printed `vX.Y.Z` means the install is behind:\n" +
			"  tell the human, who can update it with the steps on\n" +
			"  https://registerdomaincli.com (or use the connector meanwhile).",
	},
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
}

// sections are replaced whole, from their heading up to (not including) the
// next one named in end.
type section struct{ why, start, end, body string }

var skillSections = []section{
	{
		why:   "the canonical Install section downloads and runs an installer",
		start: "## Install\n",
		end:   "## Agent contract (non-interactive use)\n",
		body: "## Install\n\n" +
			"Nothing to install: this plugin's MCP connector is the way in, and its\n" +
			"tools cover every operation. The first tool call opens a browser\n" +
			"window where the human signs in with their email and the emailed\n" +
			"6-digit code; that also creates the account.\n\n" +
			"The `domain-cli` CLI is optional. Do not download or run an installer\n" +
			"yourself from this plugin. If the human wants the CLI, point them to\n" +
			"https://registerdomaincli.com for the install steps (Linux, macOS,\n" +
			"Windows) and use it once they have installed it. Everything below that\n" +
			"describes `domain-cli` applies when it is installed.\n\n",
	},
	{
		// The canonical section reads the CLI's saved token from its config
		// file to call the API with curl: reading a credential from the user's
		// machine.
		why:   "the canonical Direct API section reads the CLI's saved token",
		start: "## Direct API access\n",
		end:   "## Errors and rate limits\n",
		body: "## Direct API access\n\n" +
			"Use the MCP connector bundled with this plugin, or the CLI: between them\n" +
			"they cover the whole API. Never read the CLI's saved token (its\n" +
			"`config.json`) or any other credential from the user's machine to call\n" +
			"the API yourself; the connector signs in through OAuth in the human's\n" +
			"browser, and the CLI uses its own saved session.\n\n" +
			"Signing out is `domain-cli logout`, only when the human asks: it revokes\n" +
			"this machine's token on the server and clears it from the config.\n\n",
	},
}

func pluginSkill(src string) (string, error) {
	for _, e := range skillEdits {
		if n := strings.Count(src, e.old); n != 1 {
			return "", fmt.Errorf("skill edit %q: expected 1 match, found %d", e.why, n)
		}
		src = strings.Replace(src, e.old, e.new, 1)
	}
	for _, sec := range skillSections {
		i := strings.Index(src, sec.start)
		j := strings.Index(src, sec.end)
		if i < 0 || j < i || strings.Count(src, sec.start) != 1 || strings.Count(src, sec.end) != 1 {
			return "", fmt.Errorf("skill section %q: cannot find %q … %q", sec.why,
				strings.TrimSpace(sec.start), strings.TrimSpace(sec.end))
		}
		src = src[:i] + sec.body + src[j:]
	}
	return src, nil
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
	license, err := read("LICENSE")
	if err != nil {
		return nil, err
	}
	return []output{
		{"plugin/skills/domain-cli/SKILL.md", []byte(ps), 0o644},
		{"plugin/LICENSE", license, 0o644},
	}, nil
}

func main() {
	check := flag.Bool("check", false, "verify plugin/ is up to date instead of writing it")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if err := run(*root, *check, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "pluginbundle:", err)
		os.Exit(1)
	}
}

// errStale is returned by a -check run that found plugin/ out of date; the
// details have already been written to the report writer.
var errStale = errors.New("plugin/ is stale; run: go run ./tools/pluginbundle")

// run generates plugin/ under root, or with check verifies it without
// writing anything, reporting each problem to report.
func run(root string, check bool, report io.Writer) error {
	outs, err := build(root)
	if err != nil {
		return err
	}
	stale := false
	// Nothing but generated files may live under plugin/skills: a leftover
	// script would ship in the plugin (and scripts are refused from
	// organizations that are not Verified).
	want := map[string]bool{}
	for _, o := range outs {
		want[filepath.Clean(o.path)] = true
	}
	skillsDir := filepath.Join(root, "plugin", "skills")
	err = filepath.WalkDir(skillsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// A missing plugin/skills is fine (first generation; -check then
			// reports the missing SKILL.md). Anything else must not be
			// skipped: an unreadable directory could hide a script.
			if p == skillsDir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if want[filepath.Clean(rel)] {
			return nil
		}
		if check {
			fmt.Fprintf(report, "pluginbundle: unexpected file %s\n", filepath.ToSlash(rel))
			stale = true
			return nil
		}
		return os.Remove(p)
	})
	if err != nil {
		return err
	}
	if !check {
		if err := removeEmptyDirs(skillsDir); err != nil {
			return err
		}
	}
	for _, o := range outs {
		p := filepath.Join(root, o.path)
		if check {
			got, err := os.ReadFile(p)
			if err != nil || !bytes.Equal(got, o.data) {
				fmt.Fprintf(report, "pluginbundle: %s is stale\n", o.path)
				stale = true
				continue
			}
			if st, err := os.Stat(p); err == nil && st.Mode().Perm() != o.mode {
				fmt.Fprintf(report, "pluginbundle: %s has mode %v, want %v\n", o.path, st.Mode().Perm(), o.mode)
				stale = true
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, o.data, o.mode); err != nil {
			return err
		}
		if err := os.Chmod(p, o.mode); err != nil {
			return err
		}
	}
	if stale {
		return errStale
	}
	return nil
}

// removeEmptyDirs deletes the directories under root that the cleanup left
// empty. A directory that still has files is kept; any other error is
// returned.
func removeEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dirs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}
