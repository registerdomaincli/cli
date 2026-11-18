package main

import (
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
