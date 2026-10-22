// Package diff computes the Architecture diff between two Architecture models
// (base and head): the Modules, dependencies, operations and tables that were
// added, removed or changed, the new and resolved rule violations, and the
// notable metric changes. It also selects the changed subgraph that the PR
// image and the Mermaid fallback draw (ADR-0010).
//
// The result is deterministic: every list is sorted, nothing depends on map
// order, and violations are matched on rule and subject, never on line numbers.
package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/metrics"
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Version is the diff JSON format version.
const Version = 1

// Thresholds for a "notable" metric change. A Module's metrics are listed in
// the diff only when at least one of these trips; the rest is noise.
const (
	// DeltaInstability and DeltaDistance: |change| of I or D at or above this
	// (on a 0..1 scale) is notable.
	DeltaInstability = 0.2
	DeltaDistance    = 0.2
	// CRAPLimit: the worst-function CRAP crossing this value, in either
	// direction, is notable (CRAP above 30 is the conventional "too risky").
	CRAPLimit = 30.0
	// CRAPGrowth: the worst CRAP growing by at least this fraction is notable,
	// but only when it grew by at least CRAPGrowthFloor, so 1.0 -> 1.5 is not.
	CRAPGrowth      = 0.5
	CRAPGrowthFloor = 5.0
	// CCIncrease: the worst cyclomatic complexity rising by this much or more.
	CCIncrease = 5
)

// Subgraph caps.
const (
	// MaxNodes is the most Modules the changed subgraph draws; further ones
	// are counted in Subgraph.Omitted.
	MaxNodes = 30
	// MaxNeighbors is the most unchanged Modules added for context in total,
	// and MaxNeighborsPerModule the most per changed Module.
	MaxNeighbors          = 8
	MaxNeighborsPerModule = 3
)

// Node and edge statuses.
const (
	StatusAdded     = "added"
	StatusRemoved   = "removed"
	StatusChanged   = "changed"   // kind changed, or it owns a changed operation or table
	StatusContext   = "context"   // unchanged; drawn so the change makes sense
	StatusUnchanged = "unchanged" // for edges
)

// Diff is the Architecture diff.
type Diff struct {
	Version      int               `json:"version"`
	Changed      bool              `json:"changed"`
	Modules      ModuleChanges     `json:"modules"`
	Dependencies DependencyChanges `json:"dependencies"`
	Operations   OperationChanges  `json:"operations"`
	Tables       TableChanges      `json:"tables"`
	Violations   ViolationChanges  `json:"violations"`
	Metrics      []MetricChange    `json:"metrics"`
	Subgraph     Subgraph          `json:"subgraph"`
}

// ModuleRef names a Module and its kind.
type ModuleRef struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary,omitempty"`
}

// KindChange is a Module whose kind changed.
type KindChange struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// ModuleChanges lists Module changes.
type ModuleChanges struct {
	Added       []ModuleRef  `json:"added"`
	Removed     []ModuleRef  `json:"removed"`
	KindChanged []KindChange `json:"kind_changed"`
}

// DependencyRef is a module-to-module import.
type DependencyRef struct {
	From     string `json:"from"`
	To       string `json:"to"`
	TestOnly bool   `json:"test_only"`
}

// DependencyChange is an import that went from production to test-only or back.
type DependencyChange struct {
	From        string `json:"from"`
	To          string `json:"to"`
	WasTestOnly bool   `json:"was_test_only"`
	NowTestOnly bool   `json:"now_test_only"`
}

// DependencyChanges lists dependency changes.
type DependencyChanges struct {
	Added   []DependencyRef    `json:"added"`
	Removed []DependencyRef    `json:"removed"`
	Changed []DependencyChange `json:"changed"`
}

// OperationRef names an operation.
type OperationRef struct {
	ID     string `json:"id"`
	Owner  string `json:"owner"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

// OperationChange is an operation present in both models that differs.
type OperationChange struct {
	OperationRef
	// Changes are short phrases: "security: bearer -> public", "responses +429".
	Changes []string `json:"changes"`
}

// OperationChanges lists operation changes.
type OperationChanges struct {
	Added   []OperationRef    `json:"added"`
	Removed []OperationRef    `json:"removed"`
	Changed []OperationChange `json:"changed"`
}

// TableRef names a table.
type TableRef struct {
	Name    string `json:"name"`
	Columns int    `json:"columns"`
}

// TableChange is a table present in both models whose columns differ.
type TableChange struct {
	Name           string   `json:"name"`
	ColumnsAdded   []string `json:"columns_added"`
	ColumnsRemoved []string `json:"columns_removed"`
	ColumnsChanged []string `json:"columns_changed"`
}

// TableChanges lists table changes.
type TableChanges struct {
	Added   []TableRef    `json:"added"`
	Removed []TableRef    `json:"removed"`
	Changed []TableChange `json:"changed"`
}

// ViolationRef is a violation without its source lines (which move around).
type ViolationRef struct {
	Rule      string `json:"rule"`
	Severity  string `json:"severity"`
	Subject   string `json:"subject"`
	Module    string `json:"module"`
	To        string `json:"to,omitempty"`
	Message   string `json:"message"`
	Rationale string `json:"rationale"`
	Fix       string `json:"fix"`
}

// ViolationChanges lists violations that appeared and ones that went away.
type ViolationChanges struct {
	New      []ViolationRef `json:"new"`
	Resolved []ViolationRef `json:"resolved"`
}

// MetricChange is a Module whose metrics moved notably.
type MetricChange struct {
	Module string `json:"module"`
	// Reasons say which threshold tripped, in words.
	Reasons []string      `json:"reasons"`
	Before  MetricSummary `json:"before"`
	After   MetricSummary `json:"after"`
}

// MetricSummary is the slice of a Module's metrics the diff compares.
type MetricSummary struct {
	Instability float64  `json:"instability"`
	Distance    float64  `json:"distance"`
	Zone        string   `json:"zone"`
	WorstCC     int      `json:"worst_cc"`
	WorstCRAP   *float64 `json:"worst_crap,omitempty"`
}

// Subgraph is the changed part of the graph.
type Subgraph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	// Omitted counts changed Modules left out because of MaxNodes.
	Omitted int `json:"omitted"`
	// Levels is the kind order, outermost first (from the head model).
	Levels []string `json:"levels"`
}

// Node is a Module in the subgraph.
type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Notes are short annotations: "+1 operation", "+2 columns", "new violation".
	Notes []string `json:"notes"`
}

// Edge is a dependency in the subgraph.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	TestOnly bool   `json:"test_only"`
	Status   string `json:"status"` // added | removed | changed | unchanged
	// Violation marks an edge behind a new violation.
	Violation bool `json:"violation"`
}

// Compute returns the Architecture diff from base to head.
func Compute(base, head *model.Model) *Diff {
	base.Normalize()
	head.Normalize()
	d := &Diff{Version: Version}
	diffModules(d, base, head)
	diffDependencies(d, base, head)
	diffOperations(d, base, head)
	diffTables(d, base, head)
	diffViolations(d, base, head)
	diffMetrics(d, base, head)
	d.Changed = len(d.Modules.Added)+len(d.Modules.Removed)+len(d.Modules.KindChanged)+
		len(d.Dependencies.Added)+len(d.Dependencies.Removed)+len(d.Dependencies.Changed)+
		len(d.Operations.Added)+len(d.Operations.Removed)+len(d.Operations.Changed)+
		len(d.Tables.Added)+len(d.Tables.Removed)+len(d.Tables.Changed)+
		len(d.Violations.New)+len(d.Violations.Resolved)+len(d.Metrics) > 0
	d.Subgraph = buildSubgraph(d, base, head)
	return d
}

// JSON encodes the diff: indented, trailing newline, no HTML escaping.
func (d *Diff) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func moduleIndex(m *model.Model) map[string]*model.Module {
	out := make(map[string]*model.Module, len(m.Modules))
	for i := range m.Modules {
		out[m.Modules[i].ID] = &m.Modules[i]
	}
	return out
}

func diffModules(d *Diff, base, head *model.Model) {
	b, h := moduleIndex(base), moduleIndex(head)
	d.Modules = ModuleChanges{Added: []ModuleRef{}, Removed: []ModuleRef{}, KindChanged: []KindChange{}}
	for _, m := range head.Modules {
		if o, ok := b[m.ID]; !ok {
			d.Modules.Added = append(d.Modules.Added, ModuleRef{ID: m.ID, Kind: m.Kind, Summary: m.Summary})
		} else if o.Kind != m.Kind {
			d.Modules.KindChanged = append(d.Modules.KindChanged, KindChange{ID: m.ID, From: o.Kind, To: m.Kind})
		}
	}
	for _, m := range base.Modules {
		if _, ok := h[m.ID]; !ok {
			d.Modules.Removed = append(d.Modules.Removed, ModuleRef{ID: m.ID, Kind: m.Kind, Summary: m.Summary})
		}
	}
}

func depKey(from, to string) string { return from + ">" + to }

func diffDependencies(d *Diff, base, head *model.Model) {
	b := map[string]model.Dependency{}
	for _, x := range base.Dependencies {
		b[depKey(x.From, x.To)] = x
	}
	h := map[string]model.Dependency{}
	for _, x := range head.Dependencies {
		h[depKey(x.From, x.To)] = x
	}
	d.Dependencies = DependencyChanges{Added: []DependencyRef{}, Removed: []DependencyRef{}, Changed: []DependencyChange{}}
	for _, x := range head.Dependencies {
		o, ok := b[depKey(x.From, x.To)]
		switch {
		case !ok:
			d.Dependencies.Added = append(d.Dependencies.Added, DependencyRef{From: x.From, To: x.To, TestOnly: x.TestOnly})
		case o.TestOnly != x.TestOnly:
			d.Dependencies.Changed = append(d.Dependencies.Changed,
				DependencyChange{From: x.From, To: x.To, WasTestOnly: o.TestOnly, NowTestOnly: x.TestOnly})
		}
	}
	for _, x := range base.Dependencies {
		if _, ok := h[depKey(x.From, x.To)]; !ok {
			d.Dependencies.Removed = append(d.Dependencies.Removed, DependencyRef{From: x.From, To: x.To, TestOnly: x.TestOnly})
		}
	}
}

func opRef(o model.Operation) OperationRef {
	return OperationRef{ID: o.ID, Owner: o.Owner, Method: o.Method, Path: o.Path}
}

func securityText(s []string) string {
	if len(s) == 0 {
		return "public"
	}
	return strings.Join(s, "+")
}

func statusSet(o model.Operation) map[string]bool {
	out := map[string]bool{}
	for _, r := range o.Responses {
		out[r.Status] = true
	}
	return out
}

func signed(prefix string, set map[string]bool, other map[string]bool) []string {
	var out []string
	for k := range set {
		if !other[k] {
			out = append(out, prefix+k)
		}
	}
	sort.Strings(out)
	return out
}

// opChanges describes how an operation differs, in short phrases.
func opChanges(a, b model.Operation) []string {
	var out []string
	if a.Method != b.Method || a.Path != b.Path {
		out = append(out, fmt.Sprintf("route: %s %s -> %s %s", a.Method, a.Path, b.Method, b.Path))
	}
	if a.Owner != b.Owner {
		out = append(out, fmt.Sprintf("owner: %s -> %s", a.Owner, b.Owner))
	}
	if !reflect.DeepEqual(a.Security, b.Security) {
		out = append(out, fmt.Sprintf("security: %s -> %s", securityText(a.Security), securityText(b.Security)))
	}
	if !reflect.DeepEqual(a.RateLimit, b.RateLimit) {
		switch {
		case len(a.RateLimit) == 0:
			out = append(out, "rate limit added")
		case len(b.RateLimit) == 0:
			out = append(out, "rate limit removed")
		default:
			out = append(out, "rate limit changed")
		}
	}
	as, bs := statusSet(a), statusSet(b)
	if r := append(signed("+", bs, as), signed("-", as, bs)...); len(r) > 0 {
		out = append(out, "responses "+strings.Join(r, " "))
	}
	// Same status codes, different schemas.
	schemas := func(o model.Operation) map[string][]string {
		m := map[string][]string{}
		for _, r := range o.Responses {
			m[r.Status] = r.Schemas
		}
		return m
	}
	asch, bsch := schemas(a), schemas(b)
	var schemaChanged []string
	for st := range as {
		if bs[st] && !reflect.DeepEqual(asch[st], bsch[st]) {
			schemaChanged = append(schemaChanged, st)
		}
	}
	sort.Strings(schemaChanged)
	if len(schemaChanged) > 0 {
		out = append(out, "response schema changed for "+strings.Join(schemaChanged, " "))
	}
	if !reflect.DeepEqual(a.RequestSchemas, b.RequestSchemas) {
		out = append(out, "request schema changed")
	}
	return out
}

func diffOperations(d *Diff, base, head *model.Model) {
	b := map[string]model.Operation{}
	for _, o := range base.Operations {
		b[o.ID] = o
	}
	h := map[string]model.Operation{}
	for _, o := range head.Operations {
		h[o.ID] = o
	}
	d.Operations = OperationChanges{Added: []OperationRef{}, Removed: []OperationRef{}, Changed: []OperationChange{}}
	for _, o := range head.Operations {
		old, ok := b[o.ID]
		if !ok {
			d.Operations.Added = append(d.Operations.Added, opRef(o))
			continue
		}
		if ch := opChanges(old, o); len(ch) > 0 {
			d.Operations.Changed = append(d.Operations.Changed, OperationChange{OperationRef: opRef(o), Changes: ch})
		}
	}
	for _, o := range base.Operations {
		if _, ok := h[o.ID]; !ok {
			d.Operations.Removed = append(d.Operations.Removed, opRef(o))
		}
	}
}

func diffTables(d *Diff, base, head *model.Model) {
	b := map[string]model.Table{}
	for _, t := range base.Tables {
		b[t.Name] = t
	}
	h := map[string]model.Table{}
	for _, t := range head.Tables {
		h[t.Name] = t
	}
	d.Tables = TableChanges{Added: []TableRef{}, Removed: []TableRef{}, Changed: []TableChange{}}
	for _, t := range head.Tables {
		old, ok := b[t.Name]
		if !ok {
			d.Tables.Added = append(d.Tables.Added, TableRef{Name: t.Name, Columns: len(t.Columns)})
			continue
		}
		oc := map[string]model.Column{}
		for _, c := range old.Columns {
			oc[c.Name] = c
		}
		nc := map[string]model.Column{}
		ch := TableChange{Name: t.Name, ColumnsAdded: []string{}, ColumnsRemoved: []string{}, ColumnsChanged: []string{}}
		for _, c := range t.Columns {
			o, ok := oc[c.Name]
			switch {
			case !ok:
				ch.ColumnsAdded = append(ch.ColumnsAdded, c.Name)
			case o.Type != c.Type || o.Nullable != c.Nullable:
				ch.ColumnsChanged = append(ch.ColumnsChanged, fmt.Sprintf("%s: %s -> %s", c.Name, colText(o), colText(c)))
			}
			nc[c.Name] = c
		}
		for _, c := range old.Columns {
			if _, ok := nc[c.Name]; !ok {
				ch.ColumnsRemoved = append(ch.ColumnsRemoved, c.Name)
			}
		}
		if len(ch.ColumnsAdded)+len(ch.ColumnsRemoved)+len(ch.ColumnsChanged) > 0 {
			sort.Strings(ch.ColumnsAdded)
			sort.Strings(ch.ColumnsRemoved)
			sort.Strings(ch.ColumnsChanged)
			d.Tables.Changed = append(d.Tables.Changed, ch)
		}
	}
	for _, t := range base.Tables {
		if _, ok := h[t.Name]; !ok {
			d.Tables.Removed = append(d.Tables.Removed, TableRef{Name: t.Name, Columns: len(t.Columns)})
		}
	}
}

func colText(c model.Column) string {
	if c.Nullable {
		return c.Type + " null"
	}
	return c.Type
}

func violationRef(v model.Violation) ViolationRef {
	return ViolationRef{Rule: v.Rule, Severity: v.Severity, Subject: v.Subject, Module: v.Module, To: v.To,
		Message: v.Message, Rationale: v.Rationale, Fix: v.Fix}
}

func diffViolations(d *Diff, base, head *model.Model) {
	key := func(v model.Violation) string { return v.Rule + "|" + v.Subject }
	b := map[string]bool{}
	for _, v := range base.Violations {
		b[key(v)] = true
	}
	h := map[string]bool{}
	for _, v := range head.Violations {
		h[key(v)] = true
	}
	d.Violations = ViolationChanges{New: []ViolationRef{}, Resolved: []ViolationRef{}}
	for _, v := range head.Violations {
		if !b[key(v)] {
			d.Violations.New = append(d.Violations.New, violationRef(v))
		}
	}
	for _, v := range base.Violations {
		if !h[key(v)] {
			d.Violations.Resolved = append(d.Violations.Resolved, violationRef(v))
		}
	}
}

func summarize(m *model.Metrics) MetricSummary {
	return MetricSummary{Instability: m.Instability, Distance: m.Distance, Zone: metrics.Zone(m),
		WorstCC: m.WorstCC, WorstCRAP: m.WorstCRAP}
}

func r2(f float64) string { return fmt.Sprintf("%.2f", f) }

func diffMetrics(d *Diff, base, head *model.Model) {
	b := moduleIndex(base)
	d.Metrics = []MetricChange{}
	for _, m := range head.Modules {
		o, ok := b[m.ID]
		if !ok || o.Metrics == nil || m.Metrics == nil {
			continue
		}
		a, n := o.Metrics, m.Metrics
		var why []string
		if abs(n.Instability-a.Instability) >= DeltaInstability {
			why = append(why, fmt.Sprintf("instability %s -> %s", r2(a.Instability), r2(n.Instability)))
		}
		if abs(n.Distance-a.Distance) >= DeltaDistance {
			why = append(why, fmt.Sprintf("distance %s -> %s", r2(a.Distance), r2(n.Distance)))
		}
		if za, zn := metrics.Zone(a), metrics.Zone(n); za != zn {
			why = append(why, fmt.Sprintf("moved from %s to %s", za, zn))
		}
		if a.WorstCRAP != nil && n.WorstCRAP != nil {
			x, y := *a.WorstCRAP, *n.WorstCRAP
			switch {
			case x < CRAPLimit && y >= CRAPLimit:
				why = append(why, fmt.Sprintf("worst CRAP crossed %g (%.1f -> %.1f)", CRAPLimit, x, y))
			case x >= CRAPLimit && y < CRAPLimit:
				why = append(why, fmt.Sprintf("worst CRAP fell below %g (%.1f -> %.1f)", CRAPLimit, x, y))
			case x > 0 && y-x >= CRAPGrowthFloor && (y-x)/x >= CRAPGrowth:
				why = append(why, fmt.Sprintf("worst CRAP grew %.1f -> %.1f", x, y))
			}
		}
		if n.WorstCC-a.WorstCC >= CCIncrease {
			why = append(why, fmt.Sprintf("worst CC %d -> %d", a.WorstCC, n.WorstCC))
		}
		if len(why) > 0 {
			d.Metrics = append(d.Metrics, MetricChange{Module: m.ID, Reasons: why, Before: summarize(a), After: summarize(n)})
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
