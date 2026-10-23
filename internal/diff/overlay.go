package diff

import "github.com/Kaliki-Labs/archmodel/internal/model"

// Overlay returns the head model plus everything that exists only in base
// (removed Modules with their surface, dependencies, operations, tables), so
// the Explorer can draw removed things ghosted next to the live ones. Head's
// violations and metrics are kept as they are. The inputs are not modified.
func Overlay(base, head *model.Model) *model.Model {
	out := *head
	hMods := moduleIndex(head)
	removed := map[string]bool{}
	out.Modules = append([]model.Module(nil), head.Modules...)
	for _, m := range base.Modules {
		if hMods[m.ID] == nil {
			out.Modules = append(out.Modules, m)
			removed[m.ID] = true
		}
	}
	has := map[string]bool{}
	out.Dependencies = append([]model.Dependency(nil), head.Dependencies...)
	for _, d := range head.Dependencies {
		has[depKey(d.From, d.To)] = true
	}
	for _, d := range base.Dependencies {
		if !has[depKey(d.From, d.To)] {
			out.Dependencies = append(out.Dependencies, d)
		}
	}
	ops := map[string]bool{}
	out.Operations = append([]model.Operation(nil), head.Operations...)
	for _, o := range head.Operations {
		ops[o.ID] = true
	}
	for _, o := range base.Operations {
		if !ops[o.ID] {
			out.Operations = append(out.Operations, o)
		}
	}
	tabs := map[string]bool{}
	out.Tables = append([]model.Table(nil), head.Tables...)
	for _, t := range head.Tables {
		tabs[t.Name] = true
	}
	for _, t := range base.Tables {
		if !tabs[t.Name] {
			out.Tables = append(out.Tables, t)
		}
	}
	// The surface of removed Modules, so their cards are not empty.
	out.Symbols = append([]model.Symbol(nil), head.Symbols...)
	for _, s := range base.Symbols {
		if removed[s.Module] {
			out.Symbols = append(out.Symbols, s)
		}
	}
	out.Functions = append([]model.Function(nil), head.Functions...)
	for _, f := range base.Functions {
		if removed[f.Module] {
			out.Functions = append(out.Functions, f)
		}
	}
	out.Queries = append([]model.Query(nil), head.Queries...)
	for _, q := range base.Queries {
		if removed[q.Module] {
			out.Queries = append(out.Queries, q)
		}
	}
	out.Schemas = append([]model.Schema(nil), head.Schemas...)
	sch := map[string]bool{}
	for _, s := range head.Schemas {
		sch[s.ID] = true
	}
	for _, s := range base.Schemas {
		if !sch[s.ID] {
			out.Schemas = append(out.Schemas, s)
		}
	}
	out.Normalize()
	return &out
}

// ExplorerPayload is the object embedded as the Explorer's "archmodel-diff"
// block: ids to highlight (edges as "from>to") plus the diff itself for the
// summary card.
func (d *Diff) ExplorerPayload() map[string]any {
	ids := func(n int, f func(i int) string) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, f(i))
		}
		return out
	}
	edge := func(r []DependencyRef) []string {
		return ids(len(r), func(i int) string { return depKey(r[i].From, r[i].To) })
	}
	return map[string]any{
		"added": map[string]any{
			"modules":    ids(len(d.Modules.Added), func(i int) string { return d.Modules.Added[i].ID }),
			"edges":      edge(d.Dependencies.Added),
			"operations": ids(len(d.Operations.Added), func(i int) string { return d.Operations.Added[i].ID }),
			"tables":     ids(len(d.Tables.Added), func(i int) string { return d.Tables.Added[i].Name }),
		},
		"removed": map[string]any{
			"modules":    ids(len(d.Modules.Removed), func(i int) string { return d.Modules.Removed[i].ID }),
			"edges":      edge(d.Dependencies.Removed),
			"operations": ids(len(d.Operations.Removed), func(i int) string { return d.Operations.Removed[i].ID }),
			"tables":     ids(len(d.Tables.Removed), func(i int) string { return d.Tables.Removed[i].Name }),
		},
		"changed": map[string]any{
			"modules":    ids(len(d.Modules.KindChanged), func(i int) string { return d.Modules.KindChanged[i].ID }),
			"edges":      ids(len(d.Dependencies.Changed), func(i int) string { return depKey(d.Dependencies.Changed[i].From, d.Dependencies.Changed[i].To) }),
			"operations": ids(len(d.Operations.Changed), func(i int) string { return d.Operations.Changed[i].ID }),
			"tables":     ids(len(d.Tables.Changed), func(i int) string { return d.Tables.Changed[i].Name }),
		},
		"headline": d.headline(),
		"diff":     d,
	}
}
