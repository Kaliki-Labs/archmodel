// Package query answers the questions an agent asks the Architecture model
// instead of grepping: who depends on a Module, what an operation does, which
// queries touch a table. Every answer is a JSON-serialisable value.
package query

import (
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Version is the format version of every answer.
const Version = 1

// NotFoundError is returned for an unknown name; Suggestions are close names.
type NotFoundError struct {
	Kind       string   `json:"-"`
	Name       string   `json:"-"`
	Message    string   `json:"error"`
	DidYouMean []string `json:"did_you_mean"`
}

func (e *NotFoundError) Error() string { return e.Message }

func notFound(kind, name string, all []string) *NotFoundError {
	return &NotFoundError{Kind: kind, Name: name, Message: "unknown " + kind + " " + `"` + name + `"`, DidYouMean: suggest(name, all)}
}

// Index is a model prepared for lookups.
type Index struct {
	M    *model.Model
	mods map[string]*model.Module
}

// New indexes a model.
func New(m *model.Model) *Index {
	ix := &Index{M: m, mods: map[string]*model.Module{}}
	for i := range m.Modules {
		ix.mods[m.Modules[i].ID] = &m.Modules[i]
	}
	return ix
}

func (ix *Index) module(name string) (*model.Module, error) {
	if mod, ok := ix.mods[name]; ok {
		return mod, nil
	}
	ids := make([]string, 0, len(ix.mods))
	for id := range ix.mods {
		ids = append(ids, id)
	}
	return nil, notFound("module", name, ids)
}

func (ix *Index) levelName(mod *model.Module) string {
	if mod.Level == nil || *mod.Level >= len(ix.M.Levels) {
		return ""
	}
	return ix.M.Levels[*mod.Level]
}

// Edge is one dependency seen from one side.
type Edge struct {
	Module   string       `json:"module"`
	Kind     string       `json:"kind"`      // the other Module's kind
	TestOnly bool         `json:"test_only"` // only test files import
	Sites    []model.Site `json:"sites"`     // import lines; test:true marks _test.go files
}

func (ix *Index) edges(name string, dependents bool) []Edge {
	out := []Edge{}
	for _, d := range ix.M.Dependencies {
		other := d.To
		if dependents {
			if d.To != name {
				continue
			}
			other = d.From
		} else if d.From != name {
			continue
		}
		e := Edge{Module: other, TestOnly: d.TestOnly, Sites: d.Sites}
		if mod := ix.mods[other]; mod != nil {
			e.Kind = mod.Kind
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out
}

// DependencyAnswer is the answer to who-depends and depends-on.
type DependencyAnswer struct {
	Version int    `json:"version"`
	Module  string `json:"module"`
	// Relation is "depended_on_by" (who-depends) or "depends_on" (depends-on).
	Relation string `json:"relation"`
	Modules  []Edge `json:"modules"`
}

// WhoDepends lists the Modules that import name.
func (ix *Index) WhoDepends(name string) (any, error) {
	if _, err := ix.module(name); err != nil {
		return nil, err
	}
	return DependencyAnswer{Version, name, "depended_on_by", ix.edges(name, true)}, nil
}

// DependsOn lists the Modules name imports.
func (ix *Index) DependsOn(name string) (any, error) {
	if _, err := ix.module(name); err != nil {
		return nil, err
	}
	return DependencyAnswer{Version, name, "depends_on", ix.edges(name, false)}, nil
}

// ModuleListEntry is one row of the modules list.
type ModuleListEntry struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Level   string `json:"level,omitempty"`
	Summary string `json:"summary"`
}

// Modules lists every Module.
func (ix *Index) Modules() any {
	out := []ModuleListEntry{}
	for i := range ix.M.Modules {
		mod := &ix.M.Modules[i]
		out = append(out, ModuleListEntry{mod.ID, mod.Kind, ix.levelName(mod), mod.Summary})
	}
	return struct {
		Version int               `json:"version"`
		Modules []ModuleListEntry `json:"modules"`
	}{Version, out}
}

// OperationListEntry is one row of the operations list.
type OperationListEntry struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Owner   string `json:"owner"`
	Summary string `json:"summary,omitempty"`
}

// Operations lists every API operation.
func (ix *Index) Operations() any {
	out := []OperationListEntry{}
	for _, o := range ix.M.Operations {
		out = append(out, OperationListEntry{o.ID, o.Method, o.Path, o.Owner, o.Summary})
	}
	return struct {
		Version    int                  `json:"version"`
		Operations []OperationListEntry `json:"operations"`
	}{Version, out}
}

// RulesAnswer is the policy an agent reads before coding.
type RulesAnswer struct {
	Version          int               `json:"version"`
	Levels           []string          `json:"levels"`
	Rules            []model.Rule      `json:"rules"`
	MetricRationales map[string]string `json:"metric_rationales"`
}

// Rules returns every rule with its Rationale, plus the metric Rationales.
func (ix *Index) Rules() any {
	return RulesAnswer{Version, ix.M.Levels, ix.M.Rules, ix.M.MetricRationales}
}

// ModuleCard is the Module card as JSON.
type ModuleCard struct {
	Version     int                `json:"version"`
	ID          string             `json:"id"`
	Kind        string             `json:"kind"`
	Level       string             `json:"level,omitempty"`
	Summary     string             `json:"summary"`
	Purpose     string             `json:"purpose"`
	Dir         string             `json:"dir"`
	Present     bool               `json:"present"`
	Generated   bool               `json:"generated"`
	Surface     []model.Symbol     `json:"public_surface"`
	DependsOn   []Edge             `json:"depends_on"`
	DependedOn  []Edge             `json:"depended_on_by"`
	Owns        Owns               `json:"owns"`
	Metrics     *model.Metrics     `json:"metrics,omitempty"`
	Worst       WorstFunctions     `json:"worst_functions"`
	Subpackages []model.Subpackage `json:"subpackages"`
	Violations  []model.Violation  `json:"violations"`
}

// Owns is what a Module owns.
type Owns struct {
	Operations []OperationListEntry `json:"operations"`
	Tables     []string             `json:"tables"`
	Queries    []model.Query        `json:"queries"`
}

// WorstFunctions are the top few functions by complexity and by CRAP.
type WorstFunctions struct {
	ByCC   []FunctionEntry `json:"by_cc"`
	ByCRAP []FunctionEntry `json:"by_crap"` // empty without a coverprofile
}

// FunctionEntry is a function with its metrics.
type FunctionEntry struct {
	ID       string   `json:"id"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	CC       int      `json:"cc"`
	Coverage *float64 `json:"coverage,omitempty"`
	CRAP     *float64 `json:"crap,omitempty"`
}

const worstN = 3

// Module builds the Module card.
func (ix *Index) Module(name string) (any, error) {
	mod, err := ix.module(name)
	if err != nil {
		return nil, err
	}
	card := ModuleCard{
		Version: Version, ID: mod.ID, Kind: mod.Kind, Level: ix.levelName(mod), Summary: mod.Summary,
		Purpose: mod.Purpose, Dir: mod.Dir, Present: mod.Present, Generated: mod.Generated,
		Surface: []model.Symbol{}, DependsOn: ix.edges(name, false), DependedOn: ix.edges(name, true),
		Owns:    Owns{Operations: []OperationListEntry{}, Tables: []string{}, Queries: []model.Query{}},
		Metrics: mod.Metrics, Subpackages: mod.Subpackages, Violations: []model.Violation{},
		Worst: WorstFunctions{ByCC: []FunctionEntry{}, ByCRAP: []FunctionEntry{}},
	}
	for _, s := range ix.M.Symbols {
		if s.Module == name {
			card.Surface = append(card.Surface, s)
		}
	}
	for _, o := range ix.M.Operations {
		if o.Owner == name {
			card.Owns.Operations = append(card.Owns.Operations, OperationListEntry{o.ID, o.Method, o.Path, o.Owner, o.Summary})
		}
	}
	for _, t := range ix.M.Tables {
		for _, o := range t.Owners {
			if o == name {
				card.Owns.Tables = append(card.Owns.Tables, t.Name)
			}
		}
	}
	for _, q := range ix.M.Queries {
		if q.Module == name {
			card.Owns.Queries = append(card.Owns.Queries, q)
		}
	}
	var fns []FunctionEntry
	for _, f := range ix.M.Functions {
		if f.Module == name {
			fns = append(fns, FunctionEntry{f.ID, f.File, f.Line, f.CC, f.Coverage, f.CRAP})
		}
	}
	byCC := append([]FunctionEntry(nil), fns...)
	sort.SliceStable(byCC, func(i, j int) bool { return byCC[i].CC > byCC[j].CC })
	card.Worst.ByCC = top(byCC)
	var withCRAP []FunctionEntry
	for _, f := range fns {
		if f.CRAP != nil {
			withCRAP = append(withCRAP, f)
		}
	}
	sort.SliceStable(withCRAP, func(i, j int) bool { return *withCRAP[i].CRAP > *withCRAP[j].CRAP })
	card.Worst.ByCRAP = top(withCRAP)
	for _, v := range ix.M.Violations {
		if v.Module == name || v.To == name {
			card.Violations = append(card.Violations, v)
		}
	}
	return card, nil
}

func top(f []FunctionEntry) []FunctionEntry {
	if len(f) > worstN {
		f = f[:worstN]
	}
	if f == nil {
		return []FunctionEntry{}
	}
	return f
}

// OperationAnswer is an operation with the closure of schemas it uses.
type OperationAnswer struct {
	Version int `json:"version"`
	model.Operation
	// Schemas are the request and response schemas plus every schema they
	// reference, transitively.
	Schemas []model.Schema `json:"schemas"`
}

// Operation describes one operation by operationId.
func (ix *Index) Operation(id string) (any, error) {
	var op *model.Operation
	ids := []string{}
	for i := range ix.M.Operations {
		ids = append(ids, ix.M.Operations[i].ID)
		if ix.M.Operations[i].ID == id {
			op = &ix.M.Operations[i]
		}
	}
	if op == nil {
		return nil, notFound("operation", id, ids)
	}
	byID := map[string]model.Schema{}
	for _, s := range ix.M.Schemas {
		byID[s.ID] = s
	}
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		s, ok := byID[id]
		if !ok || seen[id] {
			return
		}
		seen[id] = true
		for _, r := range s.Refs {
			walk(r)
		}
	}
	for _, s := range op.RequestSchemas {
		walk(s)
	}
	for _, r := range op.Responses {
		for _, s := range r.Schemas {
			walk(s)
		}
	}
	schemas := []model.Schema{}
	for _, s := range ix.M.Schemas {
		if seen[s.ID] {
			schemas = append(schemas, s)
		}
	}
	return OperationAnswer{Version: Version, Operation: *op, Schemas: schemas}, nil
}

// TableAnswer is a table with the queries and Modules that touch it.
type TableAnswer struct {
	Version int `json:"version"`
	model.Table
	Queries []model.Query `json:"queries"`
	Modules []string      `json:"modules"` // owners plus the Modules of the queries
}

// Table describes one table.
func (ix *Index) Table(name string) (any, error) {
	var tb *model.Table
	names := []string{}
	for i := range ix.M.Tables {
		names = append(names, ix.M.Tables[i].Name)
		if ix.M.Tables[i].Name == name {
			tb = &ix.M.Tables[i]
		}
	}
	if tb == nil {
		return nil, notFound("table", name, names)
	}
	ans := TableAnswer{Version: Version, Table: *tb, Queries: []model.Query{}}
	mods := map[string]bool{}
	for _, o := range tb.Owners {
		mods[o] = true
	}
	for _, q := range ix.M.Queries {
		for _, t := range q.Tables {
			if t == name {
				ans.Queries = append(ans.Queries, q)
				mods[q.Module] = true
			}
		}
	}
	ans.Modules = []string{}
	for m := range mods {
		ans.Modules = append(ans.Modules, m)
	}
	sort.Strings(ans.Modules)
	return ans, nil
}

// suggest returns up to five names close to name: substring matches first,
// then small edit distances.
func suggest(name string, all []string) []string {
	type cand struct {
		s string
		d int
	}
	var cs []cand
	lname := strings.ToLower(name)
	for _, a := range all {
		la := strings.ToLower(a)
		d := levenshtein(lname, la)
		switch {
		case lname != "" && (strings.Contains(la, lname) || strings.Contains(lname, la)):
			d = 0
		case d > 2 && d > len(name)/3:
			continue
		}
		cs = append(cs, cand{a, d})
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].d != cs[j].d {
			return cs[i].d < cs[j].d
		}
		return cs[i].s < cs[j].s
	})
	out := []string{}
	for i := 0; i < len(cs) && i < 5; i++ {
		out = append(out, cs[i].s)
	}
	return out
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(b)]
}
