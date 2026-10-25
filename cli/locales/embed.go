// Package locales embeds the CLI translation bundles. The JSON files live at
// cli/locales/*.json; keeping the embed in its own package lets cli/i18n
// (a subpackage of cli) import the files without an import cycle.
package locales

import "embed"

//go:embed *.json
var FS embed.FS
