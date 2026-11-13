package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// SQLChanges lists what a change does to query scores and per-operation query
// counts (ADR-0012, ADR-0013). Only changes a reviewer should look at appear:
// queries that got worse or are new and below the threshold, queries that got
// better, operations whose estimate or findings changed.
type SQLChanges struct {
	Queries    []QueryScoreChange   `json:"queries"`
	Operations []OperationSQLChange `json:"operations"`
}

// QueryScoreChange is one query whose score moved, or a new query that scores
// below the threshold.
type QueryScoreChange struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Status string `json:"status"` // new | worse | better | changed
	From   *int   `json:"from,omitempty"`
	To     int    `json:"to"`
	Grade  string `json:"grade"`
	// Findings are the codes that appeared (worse, new) or went away (better).
	Findings []string `json:"findings"`
	// Fix is the first finding's fix hint, when the query got worse.
	Fix string `json:"fix,omitempty"`
	// Costs is the penalty each appeared finding adds (or each resolved one
	// removed), by code.
	Costs map[string]int `json:"costs,omitempty"`
	// ColumnsAdded and ColumnsRemoved are what the query now returns
	// differently: a SELECT * picking up a new table column, or an edited
	// select list. ColumnsFrom says whether the list is sqlc's generated Go
	// ("generated"), a * expanded from the migrations ("schema") or the query
	// text ("text").
	ColumnsAdded   []string `json:"columns_added,omitempty"`
	ColumnsRemoved []string `json:"columns_removed,omitempty"`
	ColumnsFrom    string   `json:"columns_from,omitempty"`
	// Edited is set when the statement text changed.
	Edited bool `json:"edited,omitempty"`
	// ReachedBy are the operations that run this query (head side), so the
	// reviewer sees who is affected.
	ReachedBy []OperationRef `json:"reached_by,omitempty"`
}

// QueryUse is one query call an operation gained or lost.
type QueryUse struct {
	Query  string   `json:"query"`
	InLoop bool     `json:"in_loop"`
	Via    []string `json:"via,omitempty"`
	File   string   `json:"file,omitempty"`
	Line   int      `json:"line,omitempty"`
}

// OperationSQLChange is an operation whose query count or SQL findings changed.
type OperationSQLChange struct {
	Operation OperationRef `json:"operation"`
	Status    string       `json:"status"` // worse | better | changed | new
	From      string       `json:"from,omitempty"`
	To        string       `json:"to"`
	Findings  []string     `json:"findings"` // finding codes that appeared (worse) or went away (better)
	Measured  string       `json:"measured,omitempty"`
	// Added and Removed are the query calls that differ: the "which query was
	// added" behind a 3 → 4.
	Added   []QueryUse `json:"added,omitempty"`
	Removed []QueryUse `json:"removed,omitempty"`
}

func statusRank(s string) int {
	switch s {
	case "better":
		return 2
	case "changed":
		return 1
	}
	return 0
}

func (c SQLChanges) empty() bool { return len(c.Queries)+len(c.Operations) == 0 }

// findingCodes indexes findings by code. n-plus-one-not-observed is a probe
// result, not a code property, so it never counts as a change: base and head
// may be built with different probe files.
func findingCodes(fs []model.Finding) map[string]model.Finding {
	m := map[string]model.Finding{}
	for _, f := range fs {
		if f.Code == "n-plus-one-not-observed" {
			continue
		}
		if _, dup := m[f.Code]; !dup {
			m[f.Code] = f
		}
	}
	return m
}

func useKey(c model.OperationQuery) string {
	if c.InLoop {
		return c.Query + "|loop"
	}
	return c.Query
}

// diffUses compares two query lists as multisets, so a query that went from
// two calls to three shows once as added.
func diffUses(oldQs, newQs []model.OperationQuery) (added, removed []QueryUse) {
	count := map[string]int{}
	for _, c := range oldQs {
		count[useKey(c)]++
	}
	for _, c := range newQs {
		k := useKey(c)
		if count[k] > 0 {
			count[k]--
			continue
		}
		added = append(added, QueryUse{Query: c.Query, InLoop: c.InLoop, Via: c.Via, File: c.File, Line: c.Line})
	}
	seen := map[string]int{}
	for _, c := range newQs {
		seen[useKey(c)]++
	}
	for _, c := range oldQs {
		k := useKey(c)
		if seen[k] > 0 {
			seen[k]--
			continue
		}
		removed = append(removed, QueryUse{Query: c.Query, InLoop: c.InLoop, Via: c.Via, File: c.File, Line: c.Line})
	}
	return added, removed
}

func setDiff(a, b []string) (onlyA []string) {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	for _, x := range a {
		if !in[x] {
			onlyA = append(onlyA, x)
		}
	}
	return onlyA
}

func diffSQL(d *Diff, base, head *model.Model) {
	d.SQL = SQLChanges{Queries: []QueryScoreChange{}, Operations: []OperationSQLChange{}}
	threshold := 0
	if head.SQL != nil {
		threshold = head.SQL.ScoreThreshold
	}
	baseQ := map[string]model.Query{}
	for _, q := range base.Queries {
		baseQ[q.ID] = q
	}
	reach := map[string][]OperationRef{}
	for _, o := range head.Operations {
		if o.SQL == nil {
			continue
		}
		seenQ := map[string]bool{}
		for _, c := range o.SQL.Queries {
			if !seenQ[c.Query] {
				seenQ[c.Query] = true
				reach[c.Query] = append(reach[c.Query], opRef(o))
			}
		}
	}
	for _, q := range head.Queries {
		if q.Score == nil {
			continue
		}
		old, had := baseQ[q.ID]
		if !had || old.Score == nil {
			if q.Score.Value < threshold {
				c := QueryScoreChange{ID: q.ID, Module: q.Module, Status: "new", To: q.Score.Value, Grade: q.Score.Grade, Findings: []string{}, Costs: map[string]int{}}
				for _, f := range q.Score.Findings {
					c.Findings = append(c.Findings, f.Code)
					c.Costs[f.Code] = f.Penalty
				}
				if len(q.Score.Findings) > 0 {
					c.Fix = q.Score.Findings[0].Fix
				}
				d.SQL.Queries = append(d.SQL.Queries, c)
			}
			continue
		}
		from, to := old.Score.Value, q.Score.Value
		added, removed := setDiff(q.Columns, old.Columns), setDiff(old.Columns, q.Columns)
		edited := old.Hash != "" && q.Hash != "" && old.Hash != q.Hash
		if from == to && len(added) == 0 && len(removed) == 0 && !edited {
			continue
		}
		c := QueryScoreChange{ID: q.ID, Module: q.Module, From: &from, To: to, Grade: q.Score.Grade, Findings: []string{}, Costs: map[string]int{},
			ColumnsAdded: added, ColumnsRemoved: removed, Edited: edited, ReachedBy: reach[q.ID]}
		if len(added)+len(removed) > 0 {
			c.ColumnsFrom = q.ColumnsFrom
		}
		was, is := findingCodes(old.Score.Findings), findingCodes(q.Score.Findings)
		switch {
		case to < from:
			c.Status = "worse"
			for _, f := range q.Score.Findings {
				if _, ok := was[f.Code]; !ok {
					c.Findings = append(c.Findings, f.Code)
					c.Costs[f.Code] = f.Penalty
					if c.Fix == "" {
						c.Fix = f.Fix
					}
				}
			}
		case to > from:
			c.Status = "better"
			for _, f := range old.Score.Findings {
				if _, ok := is[f.Code]; !ok {
					c.Findings = append(c.Findings, f.Code)
					c.Costs[f.Code] = f.Penalty
				}
			}
		default:
			c.Status = "changed"
		}
		d.SQL.Queries = append(d.SQL.Queries, c)
	}
	sort.SliceStable(d.SQL.Queries, func(i, j int) bool {
		a, b := d.SQL.Queries[i], d.SQL.Queries[j]
		if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
			return ra < rb
		}
		return a.To < b.To
	})

	baseOp := map[string]model.Operation{}
	for _, o := range base.Operations {
		baseOp[o.ID] = o
	}
	for _, o := range head.Operations {
		if o.SQL == nil {
			continue
		}
		old, had := baseOp[o.ID]
		var oldSQL *model.OperationSQL
		if had {
			oldSQL = old.SQL
		}
		is := findingCodes(o.SQL.Findings)
		// Only one side probed: the probe may have added or removed n-plus-one
		// on its own, so compare the rest.
		skew := had && oldSQL != nil && (oldSQL.Probe == nil) != (o.SQL.Probe == nil)
		if skew {
			delete(is, "n-plus-one")
		}
		c := OperationSQLChange{Operation: opRef(o), To: o.SQL.Estimate, Findings: []string{}}
		if p := o.SQL.Probe; p != nil && len(p.Runs) > 0 {
			c.Measured = fmt.Sprintf("%s: %d queries at %d rows, %d at %d rows", p.Growth, p.Runs[0].Queries, p.Runs[0].Rows, p.Runs[len(p.Runs)-1].Queries, p.Runs[len(p.Runs)-1].Rows)
		}
		if oldSQL == nil {
			if len(is) == 0 {
				continue // a new operation with no SQL finding is not news
			}
			c.Status = "new"
			for code := range is {
				c.Findings = append(c.Findings, code)
			}
			sort.Strings(c.Findings)
			d.SQL.Operations = append(d.SQL.Operations, c)
			continue
		}
		c.From = oldSQL.Estimate
		was := findingCodes(oldSQL.Findings)
		if skew {
			delete(was, "n-plus-one")
		}
		var added, gone []string
		for code := range is {
			if _, ok := was[code]; !ok {
				added = append(added, code)
			}
		}
		for code := range was {
			if _, ok := is[code]; !ok {
				gone = append(gone, code)
			}
		}
		sort.Strings(added)
		sort.Strings(gone)
		c.Added, c.Removed = diffUses(oldSQL.Queries, o.SQL.Queries)
		switch {
		case len(added) > 0:
			c.Status, c.Findings = "worse", added
		case len(gone) > 0:
			c.Status, c.Findings = "better", gone
		case oldSQL.PerItem != o.SQL.PerItem:
			c.Status = "worse"
			if o.SQL.PerItem < oldSQL.PerItem {
				c.Status = "better"
			}
		case o.SQL.Fixed > oldSQL.Fixed:
			c.Status = "worse"
		case o.SQL.Fixed < oldSQL.Fixed:
			c.Status = "better"
		case len(c.Added)+len(c.Removed) > 0:
			c.Status = "changed" // same count, different queries
		default:
			continue
		}
		d.SQL.Operations = append(d.SQL.Operations, c)
	}
	sort.SliceStable(d.SQL.Operations, func(i, j int) bool {
		a, b := d.SQL.Operations[i], d.SQL.Operations[j]
		if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
			return ra < rb
		}
		return a.Operation.ID < b.Operation.ID
	})
}

func useText(u QueryUse) string {
	t := code(u.Query)
	var notes []string
	if u.InLoop {
		notes = append(notes, "in a loop")
	}
	if len(u.Via) > 1 {
		notes = append(notes, "via "+strings.Join(u.Via[1:], " → "))
	}
	if len(notes) > 0 {
		t += " (" + strings.Join(notes, ", ") + ")"
	}
	return t
}

func usesText(sign string, us []QueryUse) string {
	var parts []string
	for i, u := range us {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("%s%d more", sign, len(us)-3))
			break
		}
		parts = append(parts, sign+" "+useText(u))
	}
	return strings.Join(parts, ", ")
}

func costText(codes []string, costs map[string]int) string {
	var parts []string
	for _, c := range codes {
		if n := costs[c]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s −%d", c, n))
		} else {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, ", ")
}

func refsText(rs []OperationRef) string {
	var parts []string
	for i, r := range rs {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("%d more", len(rs)-3))
			break
		}
		parts = append(parts, route(r))
	}
	return strings.Join(parts, ", ")
}

func (d *Diff) sqlLines() []string {
	var out []string
	for _, o := range d.SQL.Operations {
		line := route(o.Operation) + " queries per request "
		if o.From != "" && o.From != o.To {
			line += o.From + " → " + o.To
		} else {
			line += o.To
		}
		var detail []string
		if len(o.Added) > 0 {
			detail = append(detail, usesText("+", o.Added))
		}
		if len(o.Removed) > 0 {
			detail = append(detail, usesText("−", o.Removed))
		}
		if len(detail) > 0 {
			line += ": " + strings.Join(detail, ", ")
		}
		if len(o.Findings) > 0 {
			verb := "new"
			if o.Status == "better" {
				verb = "resolved"
			}
			line += fmt.Sprintf(" (%s: %s)", verb, strings.Join(o.Findings, ", "))
		}
		if o.Measured != "" {
			line += " [measured " + o.Measured + "]"
		}
		out = append(out, line)
	}
	for _, q := range d.SQL.Queries {
		line := code(q.ID) + " "
		switch q.Status {
		case "new":
			line += fmt.Sprintf("new query scores %d (%s)", q.To, q.Grade)
		case "worse":
			line += fmt.Sprintf("score %d → %d (%s)", *q.From, q.To, q.Grade)
		case "better":
			line += fmt.Sprintf("score %d → %d (%s), improved", *q.From, q.To, q.Grade)
		default:
			line += "changed"
			if q.Edited && len(q.ColumnsAdded)+len(q.ColumnsRemoved) == 0 {
				line += " (query text edited, score unchanged)"
			}
		}
		if len(q.ColumnsAdded)+len(q.ColumnsRemoved) > 0 {
			var cs []string
			for _, c := range q.ColumnsAdded {
				cs = append(cs, "+"+c)
			}
			for _, c := range q.ColumnsRemoved {
				cs = append(cs, "−"+c)
			}
			src := map[string]string{"generated": "from sqlc's generated code", "schema": "inferred from the migrations", "text": "from the query"}[q.ColumnsFrom]
			line += fmt.Sprintf(": now returns %s", strings.Join(cs, ", "))
			if src != "" {
				line += " (" + src + ")"
			}
		}
		if len(q.Findings) > 0 {
			line += ": " + costText(q.Findings, q.Costs)
		}
		if len(q.ReachedBy) > 0 && (len(q.ColumnsAdded)+len(q.ColumnsRemoved) > 0 || q.Status == "changed") {
			line += ". Reached by " + refsText(q.ReachedBy)
		}
		if q.Fix != "" {
			line += ". Fix: " + short(q.Fix, MaxFix)
		}
		out = append(out, line)
	}
	return out
}
