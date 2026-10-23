package diff

import (
	"strings"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func f(v float64) *float64 { return &v }

func mod(id, kind string, m *model.Metrics) model.Module {
	return model.Module{ID: id, Kind: kind, Metrics: m, Present: true}
}

func base() *model.Model {
	return &model.Model{
		Version: 1,
		Levels:  []string{"composition_root", "domain", "infrastructure", "foundation"},
		Modules: []model.Module{
			mod("a", "domain", &model.Metrics{InstabilityDefined: true, Instability: 0.5, Abstractness: 0.5, Distance: 0, WorstCC: 3, WorstCRAP: f(10)}),
			mod("b", "foundation", nil),
		},
		Dependencies: []model.Dependency{{From: "a", To: "b"}},
	}
}

func TestIdenticalModelsAreUnchanged(t *testing.T) {
	d := Compute(base(), base())
	if d.Changed {
		t.Fatalf("changed: %+v", d)
	}
	if got := d.Markdown(MarkdownOptions{ImageURL: "X", Mermaid: true}); got != Marker+"\n"+NoChanges+"\n" {
		t.Errorf("got %q", got)
	}
}

func TestMetricThresholds(t *testing.T) {
	cases := []struct {
		name string
		edit func(m *model.Metrics)
		want string // substring of the reason, "" = not notable
	}{
		{"small I move", func(m *model.Metrics) { m.Instability = 0.6 }, ""},
		{"big I move", func(m *model.Metrics) { m.Instability = 0.8; m.Distance = 0.1 }, "instability 0.50 -> 0.80"},
		{"big D move", func(m *model.Metrics) { m.Distance = 0.3 }, "distance"},
		{"zone change", func(m *model.Metrics) { m.Instability, m.Abstractness, m.Distance = 0.1, 0.1, 0.1 }, "zone of pain"},
		{"CRAP crosses 30", func(m *model.Metrics) { m.WorstCRAP = f(31) }, "crossed 30"},
		{"CRAP falls below 30", func(m *model.Metrics) { *m.WorstCRAP = 12 }, ""}, // base was 10: 10 -> 12 is not notable
		{"CRAP grows 50%", func(m *model.Metrics) { m.WorstCRAP = f(16) }, "worst CRAP grew"},
		{"CRAP grows little", func(m *model.Metrics) { m.WorstCRAP = f(14) }, ""},
		{"CC +5", func(m *model.Metrics) { m.WorstCC = 8 }, "worst CC 3 -> 8"},
		{"CC +4", func(m *model.Metrics) { m.WorstCC = 7 }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			head := base()
			c.edit(head.Modules[0].Metrics)
			d := Compute(base(), head)
			got := ""
			if len(d.Metrics) > 0 {
				got = strings.Join(d.Metrics[0].Reasons, "; ")
			}
			if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
				t.Errorf("reasons = %q, want %q", got, c.want)
			}
		})
	}
}

func TestViolationsMatchOnRuleAndSubjectNotLines(t *testing.T) {
	v := func(line int) model.Violation {
		return model.Violation{Rule: "r", Subject: "a -> b", Module: "a", To: "b", Sites: []model.Site{{File: "x.go", Line: line}}}
	}
	b, h := base(), base()
	b.Violations = []model.Violation{v(3)}
	h.Violations = []model.Violation{v(30)}
	if d := Compute(b, h); d.Changed {
		t.Errorf("moved line counted as change: %+v", d.Violations)
	}
	h.Violations = nil
	d := Compute(b, h)
	if len(d.Violations.Resolved) != 1 || len(d.Violations.New) != 0 {
		t.Errorf("%+v", d.Violations)
	}
}

func TestDependencyAndKindChanges(t *testing.T) {
	h := base()
	h.Modules[1].Kind = "infrastructure"
	h.Dependencies[0].TestOnly = true
	h.Modules = append(h.Modules, mod("c", "domain", nil))
	h.Dependencies = append(h.Dependencies, model.Dependency{From: "c", To: "a"})
	d := Compute(base(), h)
	if len(d.Modules.KindChanged) != 1 || len(d.Modules.Added) != 1 || len(d.Dependencies.Changed) != 1 || len(d.Dependencies.Added) != 1 {
		t.Errorf("%+v", d)
	}
	// c -> a added; b changed kind; a -> b changed.
	var ids []string
	for _, n := range d.Subgraph.Nodes {
		ids = append(ids, n.ID+":"+n.Status)
	}
	if got := strings.Join(ids, " "); got != "a:context c:added b:changed" {
		t.Errorf("nodes = %s", got)
	}
}

func TestSubgraphCapsNodes(t *testing.T) {
	b, h := base(), base()
	for i := 0; i < MaxNodes+10; i++ {
		id := string(rune('A'+i/26)) + string(rune('a'+i%26))
		h.Modules = append(h.Modules, mod(id, "infrastructure", nil))
	}
	d := Compute(b, h)
	if len(d.Subgraph.Nodes) != MaxNodes || d.Subgraph.Omitted != 10 {
		t.Errorf("%d nodes, %d omitted", len(d.Subgraph.Nodes), d.Subgraph.Omitted)
	}
	md := d.Markdown(MarkdownOptions{})
	if !strings.Contains(md, "... and 32 more") {
		t.Errorf("list not capped:\n%s", md)
	}
}

func TestDeterministic(t *testing.T) {
	h := base()
	h.Modules = append(h.Modules, mod("z", "domain", nil), mod("y", "domain", nil))
	h.Dependencies = append(h.Dependencies, model.Dependency{From: "z", To: "y"}, model.Dependency{From: "y", To: "b"})
	first, _ := Compute(base(), h).JSON()
	for i := 0; i < 20; i++ {
		again, _ := Compute(base(), h).JSON()
		if string(again) != string(first) {
			t.Fatal("JSON differs between runs")
		}
	}
}
