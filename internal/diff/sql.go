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
	Status string `json:"status"` // new | worse | better
	From   *int   `json:"from,omitempty"`
	To     int    `json:"to"`
	Grade  string `json:"grade"`
	// Findings are the codes that appeared (worse, new) or went away (better).
	Findings []string `json:"findings"`
	// Fix is the first finding's fix hint, when the query got worse.
	Fix string `json:"fix,omitempty"`
}

// OperationSQLChange is an operation whose query count or SQL findings changed.
type OperationSQLChange struct {
	Operation OperationRef `json:"operation"`
	Status    string       `json:"status"` // worse | better | new
	From      string       `json:"from,omitempty"`
	To        string       `json:"to"`
	Findings  []string     `json:"findings"` // finding codes that appeared (worse) or went away (better)
	Measured  string       `json:"measured,omitempty"`
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
	for _, q := range head.Queries {
		if q.Score == nil {
			continue
		}
		old, had := baseQ[q.ID]
		if !had || old.Score == nil {
			if q.Score.Value < threshold {
				c := QueryScoreChange{ID: q.ID, Module: q.Module, Status: "new", To: q.Score.Value, Grade: q.Score.Grade, Findings: []string{}}
				for _, f := range q.Score.Findings {
					c.Findings = append(c.Findings, f.Code)
				}
				if len(q.Score.Findings) > 0 {
					c.Fix = q.Score.Findings[0].Fix
				}
				d.SQL.Queries = append(d.SQL.Queries, c)
			}
			continue
		}
		from, to := old.Score.Value, q.Score.Value
		if from == to {
			continue
		}
		c := QueryScoreChange{ID: q.ID, Module: q.Module, From: &from, To: to, Grade: q.Score.Grade, Findings: []string{}}
		was, is := findingCodes(old.Score.Findings), findingCodes(q.Score.Findings)
		if to < from {
			c.Status = "worse"
			for _, f := range q.Score.Findings {
				if _, ok := was[f.Code]; !ok {
					c.Findings = append(c.Findings, f.Code)
					if c.Fix == "" {
						c.Fix = f.Fix
					}
				}
			}
		} else {
			c.Status = "better"
			for _, f := range old.Score.Findings {
				if _, ok := is[f.Code]; !ok {
					c.Findings = append(c.Findings, f.Code)
				}
			}
		}
		d.SQL.Queries = append(d.SQL.Queries, c)
	}
	sort.SliceStable(d.SQL.Queries, func(i, j int) bool {
		a, b := d.SQL.Queries[i], d.SQL.Queries[j]
		if (a.Status == "better") != (b.Status == "better") {
			return b.Status == "better"
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
		case o.SQL.Fixed > oldSQL.Fixed+1:
			c.Status = "worse"
		case o.SQL.Fixed < oldSQL.Fixed-1:
			c.Status = "better"
		default:
			continue
		}
		d.SQL.Operations = append(d.SQL.Operations, c)
	}
	sort.SliceStable(d.SQL.Operations, func(i, j int) bool {
		a, b := d.SQL.Operations[i], d.SQL.Operations[j]
		if (a.Status == "better") != (b.Status == "better") {
			return b.Status == "better"
		}
		return a.Operation.ID < b.Operation.ID
	})
}

func (d *Diff) sqlLines() []string {
	var out []string
	for _, o := range d.SQL.Operations {
		line := route(o.Operation) + " "
		switch {
		case o.From != "" && o.From != o.To:
			line += fmt.Sprintf("queries per request %s → %s", o.From, o.To)
		default:
			line += "queries per request " + o.To
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
		default:
			line += fmt.Sprintf("score %d → %d (%s), improved", *q.From, q.To, q.Grade)
		}
		if len(q.Findings) > 0 {
			line += ": " + strings.Join(q.Findings, ", ")
		}
		if q.Fix != "" {
			line += ". Fix: " + short(q.Fix, MaxFix)
		}
		out = append(out, line)
	}
	return out
}
