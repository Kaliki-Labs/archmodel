package extract

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

const (
	fixtures  = "../../testdata/kaliki-template"
	goldenDir = "../../testdata/golden"
)

func loadExampleConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../docs/architecture.example.yaml", false)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func buildExample(t *testing.T, name string) *model.Model {
	t.Helper()
	m, err := Build(Options{Root: filepath.Join(fixtures, name), Config: loadExampleConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestKitchenSinkProdGraph(t *testing.T) {
	m := buildExample(t, "kitchen-sink")
	got := map[string][]string{}
	for _, d := range m.Dependencies {
		if !d.TestOnly {
			got[d.From] = append(got[d.From], d.To)
		}
	}
	want := map[string]string{
		"auth":        "config database mail ratelimit",
		"items":       "database",
		"cache":       "config",
		"events":      "config",
		"mail":        "config",
		"payments":    "config",
		"push":        "config",
		"storage":     "config",
		"server":      "auth cache config events health items mail migrate observability payments push ratelimit storage",
		"cmd/server":  "server",
		"cmd/workers": "config database events observability",
		"testsupport": "migrate",
	}
	for from, w := range want {
		g := got[from]
		sort.Strings(g)
		if strings.Join(g, " ") != w {
			t.Errorf("%s -> %v, want %s", from, g, w)
		}
	}
	for from := range got {
		if _, ok := want[from]; !ok {
			t.Errorf("unexpected edges from %s: %v", from, got[from])
		}
	}
	// auth and items import testsupport only from tests.
	for _, from := range []string{"auth", "items"} {
		found := false
		for _, d := range m.Dependencies {
			if d.From == from && d.To == "testsupport" {
				found = true
				if !d.TestOnly {
					t.Errorf("%s -> testsupport must be test-only", from)
				}
			}
		}
		if !found {
			t.Errorf("missing test-only edge %s -> testsupport", from)
		}
	}
}

func TestKitchenSinkKinds(t *testing.T) {
	m := buildExample(t, "kitchen-sink")
	kinds := map[string][]string{}
	byID := map[string]model.Module{}
	for _, mod := range m.Modules {
		kinds[mod.Kind] = append(kinds[mod.Kind], mod.ID)
		byID[mod.ID] = mod
	}
	check := func(kind string, want ...string) {
		t.Helper()
		got := kinds[kind]
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s = %v, want %v", kind, got, want)
		}
	}
	check(model.KindDomain, "auth", "health", "items")
	check(model.KindTestOnly, "testsupport")
	check(model.KindCompositionRoot, "cmd/server", "cmd/workers", "migrate", "server")
	check(model.KindFoundation, "config", "database")
	if byID["payments"].Kind != model.KindInfrastructure {
		t.Errorf("payments = %s", byID["payments"].Kind)
	}
	if db := byID["database"]; db.Present || !db.Generated {
		t.Errorf("database should be a missing generated module: %+v", db)
	}
	if ts := byID["testsupport"]; ts.Level != nil || ts.Metrics != nil {
		t.Errorf("test-only module has no level or metrics: %+v", ts)
	}
	if l := byID["server"].Level; l == nil || *l != 0 {
		t.Errorf("server level: %v", l)
	}
	// Test-only edges must not count toward the production metrics.
	if mg := byID["migrate"].Metrics; mg.Ca != 1 {
		t.Errorf("migrate Ca = %d, want 1 (only server; testsupport is test-only)", mg.Ca)
	}
}

func TestKitchenSinkOperationsAndOwners(t *testing.T) {
	m := buildExample(t, "kitchen-sink")
	ops := map[string]model.Operation{}
	for _, o := range m.Operations {
		ops[o.ID] = o
	}
	li := ops["listItems"]
	if li.Owner != "items" || li.Implementation == nil || li.Implementation.Function != "items.Service.ListItems" ||
		li.Implementation.File != "backend/internal/items/service.go" || li.Implementation.Line == 0 {
		t.Errorf("listItems: %+v", li)
	}
	if ops["login"].RateLimit["key"] != "email" {
		t.Errorf("login rate limit: %v", ops["login"].RateLimit)
	}
	if len(ops["createItem"].Security) != 1 || ops["createItem"].Security[0] != "bearerAuth" {
		t.Errorf("createItem security: %v", ops["createItem"].Security)
	}
	for _, o := range m.Operations {
		if o.Implementation == nil {
			t.Errorf("operation %s is not linked to a Go method", o.ID)
		}
	}
	for _, tb := range m.Tables {
		if tb.Name == "outbox" && strings.Join(tb.Owners, ",") != "events" {
			t.Errorf("outbox owners: %v", tb.Owners)
		}
	}
	if len(m.Warnings) != 0 {
		t.Errorf("unexpected warnings: %+v", m.Warnings)
	}
}

// Without the consumer's override file, outbox.sql has no matching Module and
// the build says so rather than guessing.
func TestDefaultsAloneWarnAboutOutbox(t *testing.T) {
	cfg, err := config.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(Options{Root: filepath.Join(fixtures, "kitchen-sink"), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Warnings) != 1 || m.Warnings[0].Code != "sql-query-module-unknown" {
		t.Errorf("warnings: %+v", m.Warnings)
	}
}

func TestGolden(t *testing.T) {
	for _, name := range []string{"minimal", "jwt-full-otp", "kitchen-sink"} {
		t.Run(name, func(t *testing.T) {
			m := buildExample(t, name)
			got, err := m.JSON()
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(goldenDir, name+".model.json")
			if *update {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/extract -update)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from golden (run go test ./internal/extract -update and review the diff)", name)
			}
		})
	}
}

func TestDeterministic(t *testing.T) {
	var first []byte
	for i := 0; i < 2; i++ {
		data, err := buildExample(t, "kitchen-sink").JSON()
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
		} else if !bytes.Equal(first, data) {
			t.Error("two builds of the same tree differ")
		}
	}
	if bytes.Contains(first, []byte(os.Getenv("HOME"))) || bytes.Contains(first, []byte("/Users/")) {
		t.Error("model contains an absolute path")
	}
}

// The extractor must not write go.sum / go.mod into the consumer's tree.
func TestBuildLeavesTreeUntouched(t *testing.T) {
	buildExample(t, "kitchen-sink")
	if _, err := os.Stat(filepath.Join(fixtures, "kitchen-sink/backend/go.sum")); err == nil {
		t.Error("go.sum was created in the fixture")
	}
}

func TestCoverageAndCRAP(t *testing.T) {
	cfg := loadExampleConfig(t)
	root := filepath.Join(fixtures, "kitchen-sink")
	base, err := Build(Options{Root: root, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	var target model.Function
	for _, f := range base.Functions {
		if f.ID == "items.Service.ListItems" {
			target = f
		}
	}
	if target.ID == "" {
		t.Fatal("items.Service.ListItems not found")
	}
	if target.Coverage != nil || target.CRAP != nil {
		t.Error("coverage must be absent without a coverprofile")
	}
	// Cover the first half of the function's lines, leave the rest uncovered.
	file := "github.com/example/kitchen-sink-app/backend/internal/items/service.go"
	mid := (target.Line + target.EndLine) / 2
	prof := fmt.Sprintf("mode: set\n%s:%d.1,%d.10 3 1\n%s:%d.1,%d.10 1 0\n", file, target.Line, mid, file, mid+1, target.EndLine)
	m, err := Build(Options{Root: root, Config: cfg, Coverage: strings.NewReader(prof)})
	if err != nil {
		t.Fatal(err)
	}
	var fn model.Function
	for _, f := range m.Functions {
		if f.ID == target.ID {
			fn = f
		}
	}
	if fn.Statements == nil || *fn.Statements != 4 || *fn.Covered != 3 || *fn.Coverage != 0.75 {
		t.Fatalf("coverage: %+v", fn)
	}
	want := Round(CRAP(fn.CC, 0.75))
	if *fn.CRAP != want {
		t.Errorf("CRAP = %v, want %v", *fn.CRAP, want)
	}
	// The module rollup carries the worst CRAP across its functions.
	for _, mod := range m.Modules {
		if mod.ID == "items" {
			if mod.Metrics.WorstCRAP == nil || *mod.Metrics.WorstCRAP < *fn.CRAP {
				t.Errorf("items rollup: %+v", mod.Metrics)
			}
			// Everything not in the profile counts as uncovered, so the worst
			// CRAP there is CC^2+CC.
		}
	}
}

// ADR-0007: minimal's testsupport is imported by nobody but imports "testing".
func TestMinimalTestsupportIsTestOnly(t *testing.T) {
	m := buildExample(t, "minimal")
	for _, mod := range m.Modules {
		if mod.ID == "testsupport" {
			if mod.Kind != model.KindTestOnly || mod.Metrics != nil {
				t.Errorf("testsupport: %+v", mod)
			}
			return
		}
	}
	t.Fatal("testsupport not found")
}

func TestSummaryIsFirstSentence(t *testing.T) {
	m := buildExample(t, "kitchen-sink")
	for _, mod := range m.Modules {
		if mod.ID == "cache" {
			if mod.Summary != "Package cache provides the Redis client used for request-time caching." {
				t.Errorf("summary = %q", mod.Summary)
			}
		}
		if mod.Purpose == "" && mod.Summary != "" {
			t.Errorf("%s: summary without purpose", mod.ID)
		}
	}
}
