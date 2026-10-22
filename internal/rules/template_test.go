package rules_test

import (
	"path/filepath"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/extract"
)

// knownDefects are violations the template currently has, and which we accept
// until the template is fixed. cmd/server's main.go has no package doc comment
// in kaliki-template@104983b (the template fix is pending). The test fails if
// the defect disappears, so we remove the entry when the template is fixed and
// the fixtures are re-synced.
var knownDefects = map[string]bool{
	"package_doc cmd/server": true,
}

func TestTemplatePassesAllRules(t *testing.T) {
	cfg, err := config.Load("../../docs/architecture.example.yaml", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"minimal", "jwt-full-otp", "kitchen-sink"} {
		t.Run(name, func(t *testing.T) {
			m, err := extract.Build(extract.Options{Root: filepath.Join("../../testdata/kaliki-template", name), Config: cfg})
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Rules) != 5 {
				t.Fatalf("want 5 rules, got %d", len(m.Rules))
			}
			seen := map[string]bool{}
			for _, v := range m.Violations {
				key := v.Rule + " " + v.Subject
				if knownDefects[key] {
					seen[key] = true
					continue
				}
				t.Errorf("unexpected violation: %s %s (%s)", v.Rule, v.Subject, v.Message)
			}
			for k := range knownDefects {
				if !seen[k] {
					t.Errorf("known defect %q no longer occurs: the template was fixed, remove it from knownDefects", k)
				}
			}
		})
	}
}
