package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// KindLabel is the human name of a Module kind (CONTEXT.md).
func KindLabel(kind string) string {
	switch kind {
	case model.KindCompositionRoot:
		return "Composition root"
	case model.KindDomain:
		return "Domain"
	case model.KindInfrastructure:
		return "Infrastructure"
	case model.KindFoundation:
		return "Foundation"
	case model.KindTestOnly:
		return "Test-only"
	}
	return kind
}

// buildSubgraph selects what the image draws:
//
//  1. Modules added, removed or changed in kind.
//  2. Modules that own an added, removed or changed operation or table.
//  3. The endpoints of every added, removed or changed dependency and of every
//     dependency behind a new violation; Modules with a Module-level new
//     violation.
//  4. Context: for Modules that are only in the graph because they own a
//     changed operation or table, or changed kind, up to
//     MaxNeighborsPerModule unchanged production neighbours (one dependent and
//     up to two dependencies), at most MaxNeighbors in total.
//
// More than MaxNodes Modules are cut, least interesting first, and counted in
// Omitted. Removed Modules and edges come from base, everything else from
// head.
func buildSubgraph(d *Diff, base, head *model.Model) Subgraph {
	bMods, hMods := moduleIndex(base), moduleIndex(head)
	kindOf := func(id string) string {
		if m, ok := hMods[id]; ok {
			return m.Kind
		}
		if m, ok := bMods[id]; ok {
			return m.Kind
		}
		return ""
	}
	exists := func(id string) bool { return hMods[id] != nil || bMods[id] != nil }

	status := map[string]string{}
	notes := map[string][]string{}
	violating := map[string]bool{} // involved in a new violation
	ensure := func(id, st string) {
		if !exists(id) {
			return
		}
		if _, ok := status[id]; !ok {
			status[id] = st
		}
	}
	setStatus := func(id, st string) {
		if exists(id) {
			status[id] = st
		}
	}
	for _, m := range d.Modules.Added {
		setStatus(m.ID, StatusAdded)
	}
	for _, m := range d.Modules.Removed {
		setStatus(m.ID, StatusRemoved)
	}
	for _, k := range d.Modules.KindChanged {
		setStatus(k.ID, StatusChanged)
		notes[k.ID] = append(notes[k.ID], KindLabel(k.From)+" -> "+KindLabel(k.To))
	}

	// Owners of changed operations and tables.
	type counts struct{ added, removed, changed int }
	opN := map[string]*counts{}
	tabN := map[string]*counts{}
	bump := func(m map[string]*counts, owner string, f func(*counts)) {
		if owner == "" || !exists(owner) {
			return
		}
		if m[owner] == nil {
			m[owner] = &counts{}
		}
		f(m[owner])
	}
	for _, o := range d.Operations.Added {
		bump(opN, o.Owner, func(c *counts) { c.added++ })
	}
	for _, o := range d.Operations.Removed {
		bump(opN, o.Owner, func(c *counts) { c.removed++ })
	}
	for _, o := range d.Operations.Changed {
		bump(opN, o.Owner, func(c *counts) { c.changed++ })
	}
	owners := func(name string) []string {
		for _, t := range head.Tables {
			if t.Name == name {
				return t.Owners
			}
		}
		for _, t := range base.Tables {
			if t.Name == name {
				return t.Owners
			}
		}
		return nil
	}
	for _, t := range d.Tables.Added {
		for _, o := range owners(t.Name) {
			bump(tabN, o, func(c *counts) { c.added++ })
		}
	}
	for _, t := range d.Tables.Removed {
		for _, o := range owners(t.Name) {
			bump(tabN, o, func(c *counts) { c.removed++ })
		}
	}
	for _, t := range d.Tables.Changed {
		for _, o := range owners(t.Name) {
			bump(tabN, o, func(c *counts) { c.changed++ })
		}
	}
	ownerIDs := map[string]bool{}
	for id := range opN {
		ownerIDs[id] = true
	}
	for id := range tabN {
		ownerIDs[id] = true
	}
	sortedIDs := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, id := range sortedIDs(ownerIDs) {
		ensure(id, StatusChanged)
		for _, c := range []struct {
			n    *counts
			noun string
		}{{opN[id], "operations"}, {tabN[id], "tables"}} {
			if c.n == nil {
				continue
			}
			var parts []string
			for _, p := range []struct {
				sign string
				n    int
			}{{"+", c.n.added}, {"-", c.n.removed}, {"~", c.n.changed}} {
				if p.n > 0 {
					parts = append(parts, fmt.Sprintf("%s%d", p.sign, p.n))
				}
			}
			if len(parts) > 0 {
				notes[id] = append(notes[id], c.noun+" "+strings.Join(parts, " "))
			}
		}
	}

	// Edges.
	edges := map[string]*Edge{}
	for _, e := range d.Dependencies.Added {
		edges[depKey(e.From, e.To)] = &Edge{From: e.From, To: e.To, TestOnly: e.TestOnly, Status: StatusAdded}
	}
	for _, e := range d.Dependencies.Removed {
		edges[depKey(e.From, e.To)] = &Edge{From: e.From, To: e.To, TestOnly: e.TestOnly, Status: StatusRemoved}
	}
	for _, e := range d.Dependencies.Changed {
		edges[depKey(e.From, e.To)] = &Edge{From: e.From, To: e.To, TestOnly: e.NowTestOnly, Status: StatusChanged}
	}
	for _, v := range d.Violations.New {
		violating[v.Module] = true
		if v.To == "" {
			ensure(v.Module, StatusContext)
			notes[v.Module] = append(notes[v.Module], "new violation")
			continue
		}
		violating[v.To] = true
		k := depKey(v.Module, v.To)
		e := edges[k]
		if e == nil {
			test := false
			for _, hd := range head.Dependencies {
				if hd.From == v.Module && hd.To == v.To {
					test = hd.TestOnly
				}
			}
			e = &Edge{From: v.Module, To: v.To, TestOnly: test, Status: StatusUnchanged}
			edges[k] = e
		}
		e.Violation = true
	}
	for _, e := range edges {
		ensure(e.From, StatusContext)
		ensure(e.To, StatusContext)
	}

	// Cap the Modules, most interesting first.
	rank := func(id string) int {
		r := 0
		switch status[id] {
		case StatusAdded, StatusRemoved:
			r = 1
		case StatusChanged:
			r = 2
		default:
			r = 3
		}
		if violating[id] {
			r = 0
		}
		return r
	}
	ids := make([]string, 0, len(status))
	for id := range status {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ri, rj := rank(ids[i]), rank(ids[j]); ri != rj {
			return ri < rj
		}
		return ids[i] < ids[j]
	})
	omitted := 0
	if len(ids) > MaxNodes {
		for _, id := range ids[MaxNodes:] {
			delete(status, id)
			omitted++
		}
		ids = ids[:MaxNodes]
	}

	// Context neighbours for Modules that are in the graph without an edge
	// change explaining them.
	if room := MaxNodes - len(ids); room > 0 {
		budget := MaxNeighbors
		if room < budget {
			budget = room
		}
		prod := func(from, to string) bool {
			for _, hd := range head.Dependencies {
				if hd.From == from && hd.To == to && !hd.TestOnly {
					return true
				}
			}
			return false
		}
		sortedSeeds := append([]string(nil), ids...)
		sort.Strings(sortedSeeds)
		for _, id := range sortedSeeds {
			if budget == 0 {
				break
			}
			if status[id] != StatusChanged || hMods[id] == nil {
				continue
			}
			var deps, dependents []string
			for _, hd := range head.Dependencies {
				if hd.TestOnly {
					continue
				}
				if hd.From == id && hMods[hd.To] != nil {
					deps = append(deps, hd.To)
				}
				if hd.To == id && hMods[hd.From] != nil {
					dependents = append(dependents, hd.From)
				}
			}
			sort.Strings(deps)
			sort.Strings(dependents)
			// One dependent, then dependencies, skipping Modules already drawn
			// unless that gives a new edge to them.
			var picks []string
			take := func(list []string, n int) {
				for _, c := range list {
					if n == 0 {
						return
					}
					if _, drawn := status[c]; drawn {
						continue
					}
					picks = append(picks, c)
					n--
				}
			}
			take(dependents, 1)
			take(deps, MaxNeighborsPerModule-len(picks))
			for _, c := range picks {
				if budget == 0 {
					break
				}
				status[c] = StatusContext
				budget--
				if prod(c, id) {
					edges[depKey(c, id)] = &Edge{From: c, To: id, Status: StatusUnchanged}
				}
				if prod(id, c) {
					edges[depKey(id, c)] = &Edge{From: id, To: c, Status: StatusUnchanged}
				}
			}
		}
	}

	levelRank := map[string]int{}
	for i, k := range head.Levels {
		levelRank[k] = i
	}
	rankOfKind := func(k string) int {
		if r, ok := levelRank[k]; ok {
			return r
		}
		return len(levelRank) // test-only and unknown kinds last
	}
	sg := Subgraph{Nodes: []Node{}, Edges: []Edge{}, Omitted: omitted, Levels: head.Levels}
	if sg.Levels == nil {
		sg.Levels = []string{}
	}
	for id, st := range status {
		n := notes[id]
		if n == nil {
			n = []string{}
		}
		sg.Nodes = append(sg.Nodes, Node{ID: id, Kind: kindOf(id), Status: st, Notes: n})
	}
	sort.Slice(sg.Nodes, func(i, j int) bool {
		a, b := sg.Nodes[i], sg.Nodes[j]
		if ra, rb := rankOfKind(a.Kind), rankOfKind(b.Kind); ra != rb {
			return ra < rb
		}
		return a.ID < b.ID
	})
	for _, e := range edges {
		if _, ok := status[e.From]; !ok {
			continue
		}
		if _, ok := status[e.To]; !ok {
			continue
		}
		sg.Edges = append(sg.Edges, *e)
	}
	sort.Slice(sg.Edges, func(i, j int) bool {
		a, b := sg.Edges[i], sg.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return sg
}

// Drawable reports whether the subgraph is worth an image: at least two
// Modules (a lone box says nothing).
func (s Subgraph) Drawable() bool { return len(s.Nodes) >= 2 }
