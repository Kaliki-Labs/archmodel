// Package style holds the shared palette (ADR-0005): the Level colors,
// added/removed highlight colors and violation red. The Explorer emits it as
// CSS variables; the diff's SVG/PNG renderer reads the same values, so the two
// never drift apart.
package style

import (
	"fmt"
	"strings"
)

// Color is one named palette entry. CSS variable: --<Name>.
type Color struct {
	Name  string
	Value string
}

// Palette lists every color in a stable order. Level colors are named
// "c-<kind>" with the model's Module kind (composition_root, domain,
// infrastructure, foundation, test_only).
var Palette = []Color{
	{"c-composition_root", "#7c4dbb"},
	{"c-domain", "#1f7ae0"},
	{"c-infrastructure", "#1a9a78"},
	{"c-foundation", "#c07a10"},
	{"c-test_only", "#8a8f98"},

	{"c-operation", "#2b8fa8"},
	{"c-schema", "#5b6dc9"},
	{"c-table", "#a8572b"},
	{"c-query", "#8e7a1f"},
	{"c-subpackage", "#6b7280"},
	{"c-function", "#4b5563"},

	{"added", "#1a8f3c"},
	{"removed", "#c0392b"},
	{"changed", "#d97706"},
	{"violation", "#d92d20"},
	{"edge", "#7a828e"},
}

// Get returns the color with the given name (without the "--" prefix), or "".
func Get(name string) string {
	for _, c := range Palette {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// Level returns the color of a Module kind.
func Level(kind string) string { return Get("c-" + kind) }

// Added, Removed and Violation are the highlight colors.
func Added() string     { return Get("added") }
func Removed() string   { return Get("removed") }
func Changed() string   { return Get("changed") }
func Violation() string { return Get("violation") }

// CSSVars renders the palette as a :root rule of CSS variables.
func CSSVars() string {
	var b strings.Builder
	b.WriteString(":root{")
	for _, c := range Palette {
		fmt.Fprintf(&b, "--%s:%s;", c.Name, c.Value)
	}
	b.WriteString("}")
	return b.String()
}
