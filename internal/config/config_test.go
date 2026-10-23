package config

import (
	"strings"
	"testing"
)

func TestDefaultsLoad(t *testing.T) {
	c, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if c.Go.Dir != "backend" || len(c.Go.Roots) != 2 {
		t.Fatalf("unexpected go config: %+v", c.Go)
	}
	for _, id := range []string{"inward_only", "no_domain_to_domain", "no_import_composition_root", "test_only_out_of_prod", "package_doc"} {
		r, ok := c.Rules[id]
		if !ok || r.Severity == "" || r.Rationale == "" || r.Fix == "" {
			t.Errorf("rule %s incomplete: %+v", id, r)
		}
	}
	if len(c.SQL.QueryModules) != 0 {
		t.Errorf("defaults must not carry project mappings: %v", c.SQL.QueryModules)
	}
}

func TestMergeOverridesKeyByKey(t *testing.T) {
	c, err := Merge([]byte(`
version: 1
sql:
  query_modules: {outbox: events}
kinds:
  foundation: [config]
rules:
  package_doc:
    severity: warn
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.SQL.QueryModules["outbox"] != "events" {
		t.Error("query_modules not merged")
	}
	if c.SQL.SchemaDir != "backend/sql/schema" {
		t.Error("untouched scalar lost its default")
	}
	if got := c.Kinds.Foundation; len(got) != 1 || got[0] != "config" {
		t.Errorf("lists replace wholesale, got %v", got)
	}
	if len(c.Kinds.CompositionRoot) != 3 {
		t.Errorf("sibling list should keep default: %v", c.Kinds.CompositionRoot)
	}
	pd := c.Rules["package_doc"]
	if pd.Severity != "warn" || pd.Rationale == "" {
		t.Errorf("rule should merge key by key: %+v", pd)
	}
}

func TestUnknownKeysAreErrors(t *testing.T) {
	for name, y := range map[string]string{
		"top level": "version: 1\nbogus: true\n",
		"nested":    "version: 1\nsql:\n  schemadir: x\n",
		"in rule":   "version: 1\nrules:\n  package_doc:\n    sev: warn\n",
	} {
		_, err := Merge([]byte(y))
		if err == nil {
			t.Errorf("%s: expected error", name)
		} else if !strings.Contains(err.Error(), "not found") {
			t.Errorf("%s: error should name the unknown field: %v", name, err)
		}
	}
}

func TestBadVersionAndKind(t *testing.T) {
	if _, err := Merge([]byte("version: 2\n")); err == nil {
		t.Error("version 2 should be rejected")
	}
	if _, err := Merge([]byte("kinds:\n  overrides: {x: wizard}\n")); err == nil {
		t.Error("unknown kind override should be rejected")
	}
}

func TestThresholdsAcceptedAndIgnored(t *testing.T) {
	if _, err := Merge([]byte("thresholds:\n  max_crap: 30\n")); err != nil {
		t.Fatal(err)
	}
}

func TestMetricRationalesAndSeverity(t *testing.T) {
	c, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ca", "ce", "i", "a", "d", "cc", "crap"} {
		if c.MetricRationales[k] == "" {
			t.Errorf("no rationale for %s", k)
		}
	}
	c, err = Merge([]byte("metric_rationales:\n  crap: custom\n"))
	if err != nil || c.MetricRationales["crap"] != "custom" || c.MetricRationales["cc"] == "" {
		t.Errorf("metric rationales should merge key by key: %v %v", err, c)
	}
	if _, err := Merge([]byte("rules:\n  package_doc:\n    severity: fatal\n")); err == nil {
		t.Error("bad severity should be rejected")
	}
}
