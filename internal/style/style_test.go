package style

import (
	"regexp"
	"strings"
	"testing"
)

func TestPaletteIsValidAndEmitted(t *testing.T) {
	hex := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	seen := map[string]bool{}
	for _, c := range Palette {
		if !hex.MatchString(c.Value) || seen[c.Name] {
			t.Errorf("bad or duplicate color %+v", c)
		}
		seen[c.Name] = true
		if !strings.Contains(CSSVars(), "--"+c.Name+":"+c.Value+";") {
			t.Errorf("%s missing from CSS", c.Name)
		}
	}
	for _, k := range []string{"composition_root", "domain", "infrastructure", "foundation", "test_only"} {
		if Level(k) == "" {
			t.Errorf("no color for kind %s", k)
		}
	}
	if Violation() == "" || Added() == "" || Removed() == "" {
		t.Error("highlight colors missing")
	}
}
