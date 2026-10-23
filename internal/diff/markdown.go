package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/style"
)

// Marker is the hidden first line of the PR comment; sticky-comment actions
// find and update the comment by it.
const Marker = "<!-- archmodel-diff -->"

// NoChanges is the whole comment when nothing changed.
const NoChanges = "No architecture changes."

// Caps that keep the comment short.
const (
	// MaxListItems is the most bullets in one section; the rest become "+N more".
	MaxListItems = 8
	// MaxTargets is the most targets listed on one dependency line.
	MaxTargets = 6
	// MaxRationale and MaxFix are the longest Rationale and fix hint quoted;
	// both are cut after a whole sentence (the first is always kept).
	MaxRationale = 260
	MaxFix       = 170
)

// MarkdownOptions control the PR comment.
type MarkdownOptions struct {
	// ImageURL is the URL or relative path of the subgraph image; empty omits
	// the image.
	ImageURL string
	// Mermaid adds the folded Mermaid version of the subgraph.
	Mermaid bool
}

// Markdown renders the sticky PR comment.
func (d *Diff) Markdown(o MarkdownOptions) string {
	if !d.Changed {
		return Marker + "\n" + NoChanges + "\n"
	}
	var b strings.Builder
	b.WriteString(Marker + "\n")
	b.WriteString("### Architecture diff\n")
	b.WriteString(d.headline() + "\n")

	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n**%s**\n", title)
		for i, it := range items {
			if i == MaxListItems {
				fmt.Fprintf(&b, "- ... and %d more (see the diff JSON or the Explorer)\n", len(items)-MaxListItems)
				break
			}
			b.WriteString("- " + it + "\n")
		}
	}

	section("New violations", d.violationLines())
	section("Modules", d.moduleLines())
	section("Dependencies", d.dependencyLines())
	section("Operations", d.operationLines())
	section("Tables", d.tableLines())
	section("Metric changes", d.metricLines())

	if d.Subgraph.Drawable() {
		if o.ImageURL != "" {
			fmt.Fprintf(&b, "\n![changed subgraph](%s)\n", o.ImageURL)
		}
		if o.Mermaid {
			b.WriteString("\n<details><summary>Changed subgraph as Mermaid (text version)</summary>\n\n```mermaid\n")
			b.WriteString(d.Subgraph.Mermaid())
			b.WriteString("```\n\n</details>\n")
		}
	}
	return b.String()
}

func counts(add, rem, chg int) string {
	var p []string
	if add > 0 {
		p = append(p, fmt.Sprintf("+%d", add))
	}
	if rem > 0 {
		p = append(p, fmt.Sprintf("-%d", rem))
	}
	if chg > 0 {
		p = append(p, fmt.Sprintf("~%d", chg))
	}
	return strings.Join(p, " ")
}

func (d *Diff) headline() string {
	var parts []string
	if n := len(d.Violations.New); n > 0 {
		parts = append(parts, fmt.Sprintf("%d new %s", n, plural(n, "violation", "violations")))
	}
	for _, c := range []struct{ name, v string }{
		{"modules", counts(len(d.Modules.Added), len(d.Modules.Removed), len(d.Modules.KindChanged))},
		{"dependencies", counts(len(d.Dependencies.Added), len(d.Dependencies.Removed), len(d.Dependencies.Changed))},
		{"operations", counts(len(d.Operations.Added), len(d.Operations.Removed), len(d.Operations.Changed))},
		{"tables", counts(len(d.Tables.Added), len(d.Tables.Removed), len(d.Tables.Changed))},
	} {
		if c.v != "" {
			parts = append(parts, c.name+" "+c.v)
		}
	}
	if n := len(d.Violations.Resolved); n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s resolved", n, plural(n, "violation", "violations")))
	}
	if n := len(d.Metrics); n > 0 {
		parts = append(parts, fmt.Sprintf("%d metric %s", n, plural(n, "change", "changes")))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func code(s string) string { return "`" + s + "`" }

// short keeps whole sentences of s up to max characters (always the
// first one), on a single line.
func short(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	out := ""
	for _, sent := range splitSentences(s) {
		if out != "" && len(out)+1+len(sent) > max {
			break
		}
		if out != "" {
			out += " "
		}
		out += sent
	}
	return out
}

func splitSentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == ' ' {
			out = append(out, s[start:i+1])
			start = i + 2
		}
	}
	return append(out, s[start:])
}

func (d *Diff) violationLines() []string {
	var out []string
	for _, v := range d.Violations.New {
		out = append(out, fmt.Sprintf("**%s** %s: %s *Fix:* %s", v.Rule, code(v.Subject), short(v.Rationale, MaxRationale), short(v.Fix, MaxFix)))
	}
	return out
}

func (d *Diff) moduleLines() []string {
	var out []string
	for _, m := range d.Modules.Added {
		out = append(out, fmt.Sprintf("added %s (%s)", code(m.ID), KindLabel(m.Kind)))
	}
	for _, m := range d.Modules.Removed {
		out = append(out, fmt.Sprintf("removed %s (%s)", code(m.ID), KindLabel(m.Kind)))
	}
	for _, k := range d.Modules.KindChanged {
		out = append(out, fmt.Sprintf("%s is now %s (was %s)", code(k.ID), KindLabel(k.To), KindLabel(k.From)))
	}
	return out
}

// groupDeps lists dependencies as one line per importing Module.
func groupDeps(verb string, deps []DependencyRef) []string {
	by := map[string][]string{}
	var froms []string
	for _, e := range deps {
		if _, ok := by[e.From]; !ok {
			froms = append(froms, e.From)
		}
		t := code(e.To)
		if e.TestOnly {
			t += " (test-only)"
		}
		by[e.From] = append(by[e.From], t)
	}
	sort.Strings(froms)
	var out []string
	for _, f := range froms {
		ts := by[f]
		sort.Strings(ts)
		extra := ""
		if len(ts) > MaxTargets {
			extra = fmt.Sprintf(" and %d more", len(ts)-MaxTargets)
			ts = ts[:MaxTargets]
		}
		out = append(out, fmt.Sprintf("%s %s -> %s%s", verb, code(f), strings.Join(ts, ", "), extra))
	}
	return out
}

func (d *Diff) dependencyLines() []string {
	out := groupDeps("added", d.Dependencies.Added)
	out = append(out, groupDeps("removed", d.Dependencies.Removed)...)
	for _, c := range d.Dependencies.Changed {
		now := "production"
		if c.NowTestOnly {
			now = "test-only"
		}
		out = append(out, fmt.Sprintf("%s is now %s", code(c.From+" -> "+c.To), now))
	}
	return out
}

func route(o OperationRef) string { return code(o.Method + " " + o.Path) }

func (d *Diff) operationLines() []string {
	var out []string
	for _, o := range d.Operations.Added {
		out = append(out, fmt.Sprintf("added %s (%s, owner %s)", route(o), o.ID, code(o.Owner)))
	}
	for _, o := range d.Operations.Removed {
		out = append(out, fmt.Sprintf("removed %s (%s, owner %s)", route(o), o.ID, code(o.Owner)))
	}
	for _, o := range d.Operations.Changed {
		out = append(out, fmt.Sprintf("changed %s (%s): %s", route(o.OperationRef), o.ID, strings.Join(o.Changes, "; ")))
	}
	return out
}

func (d *Diff) tableLines() []string {
	var out []string
	for _, t := range d.Tables.Added {
		out = append(out, fmt.Sprintf("added %s (%d columns)", code(t.Name), t.Columns))
	}
	for _, t := range d.Tables.Removed {
		out = append(out, fmt.Sprintf("removed %s", code(t.Name)))
	}
	for _, t := range d.Tables.Changed {
		var p []string
		for _, c := range t.ColumnsAdded {
			p = append(p, "+"+code(c))
		}
		for _, c := range t.ColumnsRemoved {
			p = append(p, "-"+code(c))
		}
		for _, c := range t.ColumnsChanged {
			p = append(p, "~"+code(c))
		}
		out = append(out, fmt.Sprintf("%s columns: %s", code(t.Name), strings.Join(p, " ")))
	}
	return out
}

func (d *Diff) metricLines() []string {
	var out []string
	for _, m := range d.Metrics {
		out = append(out, fmt.Sprintf("%s: %s", code(m.Module), strings.Join(m.Reasons, "; ")))
	}
	return out
}

// Mermaid renders the subgraph as a Mermaid flowchart (top to bottom), styled
// with the shared palette: Level fills, added/removed outlines, red violation
// edges, dashed test-only edges.
func (s Subgraph) Mermaid() string {
	var b strings.Builder
	b.WriteString("flowchart TB\n")
	idx := map[string]string{}
	for i, n := range s.Nodes {
		idx[n.ID] = fmt.Sprintf("n%d", i)
	}
	kinds := map[string]bool{}
	for _, n := range s.Nodes {
		label := n.ID
		if n.Status == StatusRemoved {
			label += " (removed)"
		} else if n.Status == StatusAdded {
			label += " (added)"
		}
		for _, note := range n.Notes {
			label += "<br/>" + note
		}
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", idx[n.ID], label)
		kinds[n.Kind] = true
	}
	var links []string // linkStyle lines
	for i, e := range s.Edges {
		arrow := "-->"
		if e.TestOnly || e.Status == StatusRemoved {
			arrow = "-.->"
		}
		lbl := ""
		switch e.Status {
		case StatusAdded:
			lbl = "|added|"
		case StatusRemoved:
			lbl = "|removed|"
		}
		fmt.Fprintf(&b, "  %s %s%s %s\n", idx[e.From], arrow, lbl, idx[e.To])
		switch {
		case e.Violation:
			links = append(links, fmt.Sprintf("  linkStyle %d stroke:%s,stroke-width:3px", i, style.Violation()))
		case e.Status == StatusAdded:
			links = append(links, fmt.Sprintf("  linkStyle %d stroke:%s,stroke-width:2px", i, style.Added()))
		case e.Status == StatusRemoved:
			links = append(links, fmt.Sprintf("  linkStyle %d stroke:%s", i, style.Removed()))
		}
	}
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		c := style.Level(k)
		if c == "" {
			c = style.Get("c-subpackage")
		}
		name := k
		if name == "" {
			name = "unknown"
		}
		fmt.Fprintf(&b, "  classDef %s fill:%s,stroke:%s,color:#fff\n", name, c, c)
	}
	fmt.Fprintf(&b, "  classDef added stroke:%s,stroke-width:4px\n", style.Added())
	fmt.Fprintf(&b, "  classDef removed stroke:%s,stroke-width:3px,stroke-dasharray:6 3,opacity:0.7\n", style.Removed())
	fmt.Fprintf(&b, "  classDef changed stroke:%s,stroke-width:4px\n", style.Changed())
	for _, k := range ks {
		var ns []string
		for _, n := range s.Nodes {
			if n.Kind == k {
				ns = append(ns, idx[n.ID])
			}
		}
		name := k
		if name == "" {
			name = "unknown"
		}
		fmt.Fprintf(&b, "  class %s %s\n", strings.Join(ns, ","), name)
	}
	for _, st := range []string{StatusAdded, StatusRemoved, StatusChanged} {
		var ns []string
		for _, n := range s.Nodes {
			if n.Status == st {
				ns = append(ns, idx[n.ID])
			}
		}
		if len(ns) > 0 {
			fmt.Fprintf(&b, "  class %s %s\n", strings.Join(ns, ","), st)
		}
	}
	for _, l := range links {
		b.WriteString(l + "\n")
	}
	return b.String()
}
