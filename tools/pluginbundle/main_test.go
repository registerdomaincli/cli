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
	for _, want := range []string{"${CLAUDE_SKILL_DIR}/scripts/install.sh", "## Errors and rate limits"} {
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
