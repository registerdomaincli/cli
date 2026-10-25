package cli

import (
	"strings"
	"testing"

	"github.com/registerdomaincli/cli/cli/i18n"
)

// TestHelpLocalizedUnderLangEnv proves that cobra's own scaffolding words are
// localized: LANG=es_ES.UTF-8 must render Spanish Usage:/Flags:/Available
// Commands:/Global Flags: headers through the overridden usage template, and
// an unknown locale must fall back to the English defaults.
func TestHelpLocalizedUnderLangEnv(t *testing.T) {
	t.Run("spanish", func(t *testing.T) {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", "es_ES.UTF-8")
		setLocale(t, i18n.Detect())

		out := runRoot(t, []string{"--help"}, "")
		for _, want := range []string{"Uso:", "Opciones:", "Comandos disponibles:"} {
			if !strings.Contains(out, want) {
				t.Errorf("spanish --help missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Usage:") || strings.Contains(out, "Available Commands:") {
			t.Errorf("spanish --help still contains English scaffolding:\n%s", out)
		}

		// Subcommands show the localized "Global Flags:" header.
		out = runRoot(t, []string{"search", "--help"}, "")
		if !strings.Contains(out, "Opciones globales:") {
			t.Errorf("subcommand --help missing localized Global Flags:\n%s", out)
		}
	})

	t.Run("unknown locale falls back to english", func(t *testing.T) {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", "xx_XX.UTF-8")
		setLocale(t, i18n.Detect())

		out := runRoot(t, []string{"--help"}, "")
		for _, want := range []string{"Usage:", "Flags:", "Available Commands:"} {
			if !strings.Contains(out, want) {
				t.Errorf("english fallback --help missing %q:\n%s", want, out)
			}
		}
	})
}

// TestLocalizedArgErrors checks that positional-argument usage errors are
// localized rather than cobra's English defaults.
func TestLocalizedArgErrors(t *testing.T) {
	setLocale(t, "es")
	root := NewRootCmd("test")
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"search"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected arg-count error")
	}
	if !strings.Contains(out.String(), "requiere exactamente 1 argumento") {
		t.Errorf("localized arg error missing:\n%s", out.String())
	}
}
