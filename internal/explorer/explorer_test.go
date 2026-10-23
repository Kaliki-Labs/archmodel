package explorer

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/extract"
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func build(t *testing.T, root, cfgFile string) *model.Model {
	t.Helper()
	cfg, err := config.Load(cfgFile, cfgFile == "")
	if err != nil {
		t.Fatal(err)
	}
	m, err := extract.Build(extract.Options{Root: root, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func kitchenSink(t *testing.T) *model.Model {
	return build(t, "../../testdata/kaliki-template/kitchen-sink", "../../docs/architecture.example.yaml")
}

var block = regexp.MustCompile(`(?s)<script type="application/json" id="archmodel-model">(.*?)</script>`)

func TestKitchenSinkIsSmallAndSelfContained(t *testing.T) {
	html, err := Render(kitchenSink(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(html) > 2<<20 {
		t.Errorf("explorer HTML is %d bytes, budget is 2 MiB", len(html))
	}
	s := string(html)
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)<script[^>]+\bsrc\s*=`),
		regexp.MustCompile(`(?i)<link[^>]+\bhref\s*=`),
		regexp.MustCompile(`(?i)\b(src|href)\s*=\s*["']?(https?:)?//`),
		regexp.MustCompile(`(?i)@import`),
		regexp.MustCompile(`(?i)url\(\s*["']?https?:`),
	} {
		if loc := bad.FindStringIndex(s); loc != nil {
			t.Errorf("external reference matching %s: %q", bad, s[loc[0]:min(loc[1]+40, len(s))])
		}
	}
	if strings.Contains(s, "@MODEL_JSON@") || strings.Contains(s, "/*@") || strings.Contains(s, "<!--@") {
		t.Error("unreplaced placeholder")
	}
	// Every script element is closed exactly once: nothing inlined (library
	// or model) can end a block early.
	if o, c := strings.Count(s, "<script"), strings.Count(s, "</script>"); o != c || o != 4 {
		t.Errorf("%d <script> openings, %d closings, want 4 each", o, c)
	}
	for _, want := range []string{"--c-composition_root:", "--c-domain:", "--c-infrastructure:", "--c-foundation:", "--violation:", "--added:", "--removed:"} {
		if !strings.Contains(s, want) {
			t.Errorf("palette variable %s missing", want)
		}
	}
}

func TestEmbeddedModelRoundTrips(t *testing.T) {
	m := kitchenSink(t)
	html, err := Render(m, Options{SourceURLTemplate: "https://example.com/{ref}/{path}#L{line}", Ref: "main", Live: true})
	if err != nil {
		t.Fatal(err)
	}
	sub := block.FindSubmatch(html)
	if sub == nil {
		t.Fatal("no embedded model block")
	}
	var got model.Model
	if err := json.Unmarshal(sub[1], &got); err != nil {
		t.Fatalf("embedded model is not valid JSON: %v", err)
	}
	want, _ := m.JSON()
	have, _ := got.JSON()
	if !bytes.Equal(want, have) {
		t.Error("embedded model differs from the model after a JSON round trip")
	}
	cfgBlock := regexp.MustCompile(`(?s)id="archmodel-config">(.*?)</script>`).FindSubmatch(html)
	if cfgBlock == nil || !strings.Contains(string(cfgBlock[1]), `"ref":"main"`) || !strings.Contains(string(cfgBlock[1]), `"live":true`) {
		t.Errorf("page config: %s", cfgBlock)
	}
}

func TestModelTextCannotBreakOutOfTheScriptBlock(t *testing.T) {
	m := &model.Model{Version: 1, Modules: []model.Module{{ID: "x", Kind: "domain", Purpose: "</script><script>alert(1)</script><!--   &"}}}
	html, err := Render(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>alert(1)") || strings.Contains(string(html), "</script><script>alert") {
		t.Fatal("model text was embedded unescaped")
	}
	sub := block.FindSubmatch(html)
	var got model.Model
	if err := json.Unmarshal(sub[1], &got); err != nil {
		t.Fatal(err)
	}
	if got.Modules[0].Purpose != m.Modules[0].Purpose {
		t.Errorf("purpose changed: %q", got.Modules[0].Purpose)
	}
}

// The template has no edge violations, so the teaching path is exercised with
// a synthetic project where a Domain imports another Domain.
func TestSyntheticViolationModelIsEmbedded(t *testing.T) {
	m := build(t, "../../testdata/synthetic/domain-import", "")
	rules := map[string]bool{}
	for _, v := range m.Violations {
		rules[v.Rule] = true
		if v.Rationale == "" || v.Fix == "" {
			t.Errorf("violation without Rationale or fix: %+v", v)
		}
	}
	if !rules["no_domain_to_domain"] || !rules["inward_only"] {
		t.Fatalf("expected a Domain->Domain and an inward-only violation, got %v", rules)
	}
	html, err := Render(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got model.Model
	if err := json.Unmarshal(block.FindSubmatch(html)[1], &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Violations) != len(m.Violations) {
		t.Errorf("embedded %d violations, want %d", len(got.Violations), len(m.Violations))
	}
}

func TestDiffSeam(t *testing.T) {
	html, err := Render(&model.Model{Version: 1}, Options{Diff: map[string]any{"added": map[string]any{"modules": []string{"x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `id="archmodel-diff">{"added":{"modules":["x"]}}`) {
		t.Error("diff block not embedded")
	}
}

func TestJavaScriptSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if out, err := exec.Command(node, "--check", filepath.Join("assets", "explorer.js")).CombinedOutput(); err != nil {
		t.Fatalf("node --check: %v\n%s", err, out)
	}
}
