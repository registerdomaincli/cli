package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real sources must build, and the plugin skill must not carry the two
// things the directory policy flags: a download-and-run installer and a
// recipe that reads the CLI's saved token.
func TestPluginSkillFromRepo(t *testing.T) {
	outs, err := build("../..")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(outs[0].data)
	for _, banned := range []string{"install.sh | sh", "config.json\")", "Authorization: Bearer \" + token"} {
		if strings.Contains(skill, banned) {
			t.Errorf("plugin skill still contains %q", banned)
		}
	}
	// No bundled scripts at all (refused from non-Verified organizations),
	// and the skill must never tell the agent to install anything itself.
	if len(outs) != 2 {
		t.Errorf("bundle has %d files, want SKILL.md and LICENSE only", len(outs))
	}
	for _, banned := range []string{"curl -fsSL", "install.sh | sh", "scripts/install.sh", "install it yourself", "go install", "install the CLI (see above)"} {
		if strings.Contains(skill, banned) {
			t.Errorf("plugin skill still contains %q", banned)
		}
	}
	for _, want := range []string{"Nothing to install", "never install", "## Errors and rate limits"} {
		if !strings.Contains(skill, want) {
			t.Errorf("plugin skill lacks %q", want)
		}
	}
}

func TestEditsFailLoudly(t *testing.T) {
	if _, err := pluginSkill("no matching text"); err == nil {
		t.Fatal("expected an error when an edit no longer matches")
	}
}

// End to end on a scratch repository: check mode reports a stray script
// without deleting it, generation removes it (and its empty directory) while
// keeping the generated skill, and the next check passes.
func TestStrayScriptLifecycle(t *testing.T) {
	root := t.TempDir()
	for src, dst := range map[string]string{
		"../../skills/domain-cli/SKILL.md": "skills/domain-cli/SKILL.md",
		"../../LICENSE":                    "LICENSE",
	} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, dst)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dst), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var report bytes.Buffer
	if err := run(root, false, &report); err != nil {
		t.Fatalf("initial generation: %v", err)
	}
	skill := filepath.Join(root, "plugin/skills/domain-cli/SKILL.md")
	scripts := filepath.Join(root, "plugin/skills/domain-cli/scripts")
	stray := filepath.Join(scripts, "install.sh")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	report.Reset()
	if err := run(root, true, &report); !errors.Is(err, errStale) {
		t.Fatalf("check with a stray script: got %v, want errStale", err)
	}
	if !strings.Contains(report.String(), "plugin/skills/domain-cli/scripts/install.sh") {
		t.Errorf("check did not name the stray file: %q", report.String())
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("check mode must not delete anything: %v", err)
	}

	if err := run(root, false, &report); err != nil {
		t.Fatalf("generation: %v", err)
	}
	if _, err := os.Stat(scripts); !os.IsNotExist(err) {
		t.Fatalf("generation kept the scripts directory: %v", err)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("generation lost the skill: %v", err)
	}

	report.Reset()
	if err := run(root, true, &report); err != nil {
		t.Fatalf("check after generation: %v (%s)", err, report.String())
	}
}
