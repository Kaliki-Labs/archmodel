package extract

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/Kaliki-Labs/archmodel/internal/model"
	"github.com/Kaliki-Labs/archmodel/probe"
)

// ParseProbe reads a probe file written by the probe package.
func ParseProbe(r io.Reader) (*probe.File, error) {
	var f probe.File
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("probe file: %w", err)
	}
	if f.Version != probe.Version {
		return nil, fmt.Errorf("probe file: version %d, want %d", f.Version, probe.Version)
	}
	return &f, nil
}

// classifyProbe turns measured runs into a growth class and a slope (extra
// queries per extra row). It needs two runs at distinct sizes.
func classifyProbe(runs []probe.Run) (growth string, slope float64, ok bool) {
	rs := append([]probe.Run(nil), runs...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Rows < rs[j].Rows })
	var pts []probe.Run
	for _, r := range rs {
		if len(pts) == 0 || r.Rows != pts[len(pts)-1].Rows {
			pts = append(pts, r)
		}
	}
	if len(pts) < 2 {
		return "", 0, false
	}
	first, last := pts[0], pts[len(pts)-1]
	slope = float64(last.Queries-first.Queries) / float64(last.Rows-first.Rows)
	if math.Abs(slope) < 1e-9 {
		return "constant", 0, true
	}
	// Segment slopes: a steady slope is linear; a slope that keeps climbing
	// (each segment steeper than the one before by half or more) is superlinear.
	growth = "linear"
	prev := 0.0
	for i := 1; i < len(pts); i++ {
		seg := float64(pts[i].Queries-pts[i-1].Queries) / float64(pts[i].Rows-pts[i-1].Rows)
		if i > 1 && prev > 0 && seg > prev*1.5 {
			growth = "superlinear"
		}
		prev = seg
	}
	return growth, math.Round(slope*100) / 100, true
}

// applyProbe merges measured runs into the operations by id. Unknown ids are
// returned so the caller can say so; a probe for an operation with no static
// footprint still attaches (the footprint is only a suspicion, the probe is
// the proof).
func (b *builder) applyProbe(f *probe.File) (unknown []string) {
	byID := map[string]*model.Operation{}
	for i := range b.m.Operations {
		byID[b.m.Operations[i].ID] = &b.m.Operations[i]
	}
	for _, id := range f.OperationIDs() {
		op := byID[id]
		if op == nil {
			unknown = append(unknown, id)
			continue
		}
		in := f.Operations[id]
		growth, slope, ok := classifyProbe(in.Runs)
		if !ok {
			continue
		}
		if op.SQL == nil {
			op.SQL = &model.OperationSQL{Queries: []model.OperationQuery{}, Estimate: "?", Findings: []model.Finding{}}
		}
		p := &model.Probe{Growth: growth, Slope: slope}
		for _, r := range in.Runs {
			p.Runs = append(p.Runs, model.ProbeRun{Rows: r.Rows, Queries: r.Queries, Statements: r.Statements})
		}
		sort.Slice(p.Runs, func(i, j int) bool { return p.Runs[i].Rows < p.Runs[j].Rows })
		op.SQL.Probe = p
		op.SQL.Findings = reconcileProbe(op.SQL.Findings, p)
	}
	return unknown
}

func reconcileProbe(fs []model.Finding, p *model.Probe) []model.Finding {
	hadStatic := false
	out := fs[:0:0]
	for _, f := range fs {
		if f.Code == "n-plus-one" {
			hadStatic = true
			if p.Growth == "constant" {
				continue // measured: the loop does not query
			}
			f.Message += " (confirmed by probe)"
		}
		out = append(out, f)
	}
	last := p.Runs[len(p.Runs)-1]
	first := p.Runs[0]
	switch {
	case p.Growth != "constant" && !hadStatic:
		out = append(out, model.Finding{
			Code: "n-plus-one", Severity: "warn",
			Message:   fmt.Sprintf("measured: %d queries at %d rows, %d at %d rows (%s growth, %.2g per row)", first.Queries, first.Rows, last.Queries, last.Rows, p.Growth, p.Slope),
			Rationale: footprintRationale["n-plus-one"],
			Fix:       "fetch the rows in one query (WHERE id = ANY($1), or a JOIN) and group them in Go",
		})
	case p.Growth == "constant" && hadStatic:
		out = append(out, model.Finding{
			Code: "n-plus-one-not-observed", Severity: "info",
			Message:   fmt.Sprintf("a loop reaches a query, but the probe ran %d queries at both %d and %d rows", last.Queries, first.Rows, last.Rows),
			Rationale: "The static footprint counts every loop that contains a query call; the probe measured the real request, so the loop is bounded or the branch was not taken.",
			Fix:       "none; keep the probe in the test suite so a regression shows up",
		})
	}
	return out
}
