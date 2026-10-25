package i18n

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"testing"

	"github.com/registerdomaincli/cli/cli/locales"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name                    string
		lcAll, lcMessages, lang string
		want                    string
	}{
		{"LC_ALL es_ES wins", "es_ES.UTF-8", "fr_FR.UTF-8", "en_US.UTF-8", "es"},
		{"unknown language falls back to en", "xx_XX.UTF-8", "", "", "en"},
		{"pt_BR matches pt", "pt_BR.UTF-8", "", "", "pt"},
		{"LC_MESSAGES used when LC_ALL empty", "", "fr_FR.UTF-8", "en_US.UTF-8", "fr"},
		{"LANG used when others empty", "", "", "de_DE.UTF-8", "de"},
		{"bare code", "ja", "", "", "ja"},
		{"C locale", "C", "", "", "en"},
		{"POSIX locale", "POSIX.UTF-8", "", "", "en"},
		{"all empty", "", "", "", "en"},
		{"ca detected", "ca_ES.UTF-8", "", "", "ca"},
		{"gl detected", "gl_ES.UTF-8", "", "", "gl"},
		{"eu detected", "eu_ES.UTF-8", "", "", "eu"},
		{"zh-CN matches zh", "zh_CN.UTF-8", "", "", "zh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LC_ALL", tt.lcAll)
			t.Setenv("LC_MESSAGES", tt.lcMessages)
			t.Setenv("LANG", tt.lang)
			if got := Detect(); got != tt.want {
				t.Errorf("Detect() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := map[string]string{
		"es_ES.UTF-8": "es",
		"en_US":       "en",
		"pt_BR":       "pt",
		"zh_TW":       "zh",
		"gl_ES.UTF-8": "gl",
		"eu_ES":       "eu",
		"xx_YY":       "en",
		"C":           "en",
		"":            "en",
		"garbage!!!":  "en",
	}
	for in, want := range tests {
		if got := Match(in); got != want {
			t.Errorf("Match(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEverySupportedLocaleMatchesItself pins Supported and supportedTags to
// each other. A locale added to one and not the other leaves its bundle
// embedded but unreachable, and what the user gets instead is not English:
// NewMatcher routes the code to the nearest tag it was given, so dropping "gl"
// from supportedTags sends a gl_ES environment to Spanish, silently and with
// every string in gl.json still sitting unread in the binary. ("en" is the
// fallback for input the matcher cannot place at all, which TestMatch pins
// with "garbage!!!" — that fallback is the intended answer to input naming no
// language at all, not this one.)
func TestEverySupportedLocaleMatchesItself(t *testing.T) {
	for _, code := range Supported {
		if got := Match(code); got != code {
			t.Errorf("Match(%q) = %q: %q is in Supported but has no entry in supportedTags",
				code, got, code)
		}
	}
}

var placeholderRe = regexp.MustCompile(`\{\{\.(\w+)\}\}`)

func placeholders(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

func flatten(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("bundle is not a flat string map: %v", err)
	}
	return raw
}

// TestLocaleParity enforces the plan's locale contract: every non-en bundle
// has exactly the en keyset, and every message keeps the same template
// placeholders (a translation that drops {{.Seconds}} would crash at
// runtime, not at test time otherwise).
func TestLocaleParity(t *testing.T) {
	enData, err := locales.FS.ReadFile("en.json")
	if err != nil {
		t.Fatalf("en.json missing: %v", err)
	}
	en := flatten(t, enData)
	if len(en) == 0 {
		t.Fatal("en.json is empty")
	}
	entries, err := fs.ReadDir(locales.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		name := e.Name()
		if len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		seen++
		code := name[:len(name)-5]
		data, err := locales.FS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got := flatten(t, data)
		for key := range en {
			if _, ok := got[key]; !ok {
				t.Errorf("%s: missing key %q", code, key)
			}
		}
		for key := range got {
			if _, ok := en[key]; !ok {
				t.Errorf("%s: extra key %q", code, key)
			}
		}
		if code == "en" {
			continue
		}
		for key, msg := range got {
			if placeholders(msg) != nil || placeholders(en[key]) != nil {
				if !equalPlaceholders(placeholders(msg), placeholders(en[key])) {
					t.Errorf("%s: %s placeholders differ: got %v, en has %v",
						code, key, placeholderKeys(msg), placeholderKeys(en[key]))
				}
			}
		}
	}
	if seen != len(Supported) {
		t.Errorf("expected %d locale files, found %d", len(Supported), seen)
	}
}

func equalPlaceholders(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func placeholderKeys(s string) []string {
	var keys []string
	for k := range placeholders(s) {
		keys = append(keys, k)
	}
	return keys
}

func TestSetLocaleAndT(t *testing.T) {
	defer SetLocale("en")
	SetLocale("es")
	if Current() != "es" {
		t.Fatalf("Current() = %q, want es", Current())
	}
	if got := T("tmpl.usage"); got != "Uso:" {
		t.Errorf("T(es tmpl.usage) = %q", got)
	}
	SetLocale("xx") // unknown falls back to en
	if got := T("tmpl.usage"); got != "Usage:" {
		t.Errorf("unknown locale did not fall back to en: %q", got)
	}
	if got := T("err.ratelimited", "Seconds", 5); got != "Rate limited, retry after 5s." {
		t.Errorf("template data not applied: %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key should return the key itself, got %q", got)
	}
}
