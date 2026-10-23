package diffimage

import (
	"bytes"
	"encoding/xml"
	"image/png"
	"io"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/diff"
)

func sample() diff.Subgraph {
	return diff.Subgraph{
		Levels: []string{"composition_root", "domain", "infrastructure", "foundation"},
		Nodes: []diff.Node{
			{ID: "server", Kind: "composition_root", Status: "context", Notes: []string{}},
			{ID: "auth", Kind: "domain", Status: "context", Notes: []string{}},
			{ID: "items", Kind: "domain", Status: "changed", Notes: []string{"operations +1"}},
			{ID: "push", Kind: "infrastructure", Status: "removed", Notes: []string{}},
			{ID: "new", Kind: "infrastructure", Status: "added", Notes: []string{}},
			{ID: "config", Kind: "foundation", Status: "context", Notes: []string{}},
			{ID: "testsupport", Kind: "test_only", Status: "context", Notes: []string{}},
		},
		Edges: []diff.Edge{
			{From: "server", To: "push", Status: "removed"},
			{From: "server", To: "new", Status: "added"},
			{From: "items", To: "auth", Status: "added", Violation: true},
			{From: "new", To: "config", Status: "added"},
			{From: "testsupport", To: "config", Status: "unchanged", TestOnly: true},
		},
	}
}

func TestRowsFollowLevels(t *testing.T) {
	l := Compute(sample())
	y := map[string]float64{}
	for _, n := range l.Nodes {
		y[n.ID] = n.Y + n.H/2
	}
	if !(y["server"] < y["items"] && y["items"] < y["new"] && y["new"] < y["config"] && y["config"] < y["testsupport"]) {
		t.Errorf("rows out of order: %v", y)
	}
	if y["items"] != y["auth"] {
		t.Error("Domains should share a row")
	}
}

func TestSVGIsWellFormedAndDeterministic(t *testing.T) {
	a, b := Compute(sample()).SVG(), Compute(sample()).SVG()
	if !bytes.Equal(a, b) {
		t.Error("SVG differs between runs")
	}
	dec := xml.NewDecoder(bytes.NewReader(a))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPNGDecodesAtTwoTimes(t *testing.T) {
	l := Compute(sample())
	data, err := l.PNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if w := img.Bounds().Dx(); w < 600 || w > 1200 {
		t.Errorf("width %d", w)
	}
	if len(data) < 10_000 {
		t.Errorf("suspiciously small PNG: %d bytes", len(data))
	}
}

func TestWideRankWraps(t *testing.T) {
	s := diff.Subgraph{Levels: []string{"infrastructure"}}
	for i := 0; i < 30; i++ {
		s.Nodes = append(s.Nodes, diff.Node{ID: "module" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: "infrastructure", Status: "added", Notes: []string{}})
	}
	l := Compute(s)
	if l.W > MaxRowWidth+2*Margin {
		t.Errorf("canvas %v wide", l.W)
	}
}
