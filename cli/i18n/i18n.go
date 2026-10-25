// Package i18n localizes every user-facing CLI string.
//
// Detect() parses LC_ALL > LC_MESSAGES > LANG and matches the value against
// the supported locale set with golang.org/x/text/language, falling back to
// English. The locale is resolved in init() — before any cobra command is
// constructed — because cobra captures Short/Long strings at registration
// time. Cobra's own scaffolding words ("Usage:", "Flags:", ...) are not
// translatable through field values; cli/root.go overrides the usage template
// with words from this package instead.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/registerdomaincli/cli/cli/locales"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Supported lists the locked locale set (plan step 14). "en" is canonical.
// Supported and supportedTags are one list in two forms: same languages, same
// order, one entry per embedded bundle.
var Supported = []string{"en", "zh", "hi", "es", "fr", "ar", "pt", "ru", "de", "ja", "ca", "gl", "eu"}

// x/text names a tag constant only for the languages its own tables single
// out; Galician and Basque have none, so they are parsed from the same code
// Supported carries. MustParse is the right shape here: both are compile-time
// constants, and a typo is a packaging error this package already answers with
// a panic in newBundle.
var supportedTags = []language.Tag{
	language.English, language.Chinese, language.Hindi, language.Spanish,
	language.French, language.Arabic, language.Portuguese, language.Russian,
	language.German, language.Japanese, language.Catalan,
	language.MustParse("gl"), language.MustParse("eu"),
}

var (
	bundle  *i18n.Bundle
	current = language.English
)

func init() {
	bundle = newBundle()
	SetLocale(Detect())
}

func newBundle() *i18n.Bundle {
	b := i18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, loc := range Supported {
		data, err := locales.FS.ReadFile(loc + ".json")
		if err != nil {
			panic(fmt.Sprintf("i18n: embedded locale %s.json missing: %v", loc, err))
		}
		b.MustParseMessageFileBytes(data, loc+".json")
	}
	return b
}

// Detect returns the locale for the current environment: LC_ALL, then
// LC_MESSAGES, then LANG, matched against Supported with "en" as fallback.
func Detect() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			return Match(v)
		}
	}
	return "en"
}

// Match maps a single environment-locale value (e.g. "es_ES.UTF-8") to one
// of the supported locales. Unknown or unparseable values map to "en".
func Match(value string) string {
	v := strings.TrimSpace(value)
	if i := strings.IndexAny(v, ".@"); i >= 0 {
		v = v[:i]
	}
	if v == "" || v == "C" || v == "POSIX" {
		return "en"
	}
	tag, err := language.Parse(strings.ReplaceAll(v, "_", "-"))
	if err != nil {
		return "en"
	}
	m := language.NewMatcher(supportedTags)
	matched, _, _ := m.Match(tag)
	base, _ := matched.Base()
	code := base.String()
	for _, loc := range Supported {
		if loc == code {
			return code
		}
	}
	return "en"
}

// SetLocale switches the active locale. Unknown locales fall back to "en".
// Tests call this to simulate a detected environment locale.
func SetLocale(lang string) {
	current = language.Make(Match(lang))
}

// Current reports the active locale code (one of Supported).
func Current() string {
	base, _ := current.Base()
	return base.String()
}

// T returns the localized string for key. Arguments are name/value pairs
// feeding the message template: T("key", "Domain", "acme.example") fills
// {{.Domain}}. An unknown key returns the key itself, which keeps a missing
// translation visible instead of silent.
func T(key string, args ...any) string {
	data := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok {
			data[name] = args[i+1]
		}
	}
	l := i18n.NewLocalizer(bundle, current.String())
	msg, err := l.Localize(&i18n.LocalizeConfig{MessageID: key, TemplateData: data})
	if err != nil {
		return key
	}
	return msg
}
