package extract

import (
	"math"
	"strings"
	"testing"
)

func TestCRAP(t *testing.T) {
	cases := []struct {
		cc   int
		cov  float64
		want float64
	}{
		{1, 1, 1},
		{1, 0, 2},
		{5, 1, 5},
		{5, 0, 30},      // 25*1 + 5
		{10, 0.5, 22.5}, // 100*0.125 + 10
		{20, 0.8, 23.2}, // 400*0.008 + 20
	}
	for _, c := range cases {
		if got := CRAP(c.cc, c.cov); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("CRAP(%d, %v) = %v, want %v", c.cc, c.cov, got, c.want)
		}
	}
}

const profile = `mode: set
ex.com/app/backend/internal/a/a.go:10.20,12.3 2 1
ex.com/app/backend/internal/a/a.go:12.3,14.4 3 0
ex.com/app/backend/internal/a/a.go:20.1,22.2 4 1
ex.com/app/backend/internal/a/a.go:20.1,22.2 4 0
`

func TestParseCoverProfileAndFuncCoverage(t *testing.T) {
	p, err := ParseCoverProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatal(err)
	}
	f := "ex.com/app/backend/internal/a/a.go"
	if len(p[f]) != 3 {
		t.Fatalf("duplicate block should merge, got %d blocks", len(p[f]))
	}
	stmts, cov, ok := p.FuncCoverage(f, 10, 15)
	if !ok || stmts != 5 || cov != 2 {
		t.Errorf("func 10-15: stmts=%d covered=%d ok=%v", stmts, cov, ok)
	}
	stmts, cov, _ = p.FuncCoverage(f, 19, 23)
	if stmts != 4 || cov != 4 { // merged: count 1+0 > 0
		t.Errorf("func 19-23: stmts=%d covered=%d", stmts, cov)
	}
	if _, _, ok := p.FuncCoverage("other.go", 1, 100); ok {
		t.Error("unknown file must report ok=false")
	}
}

func TestParseCoverProfileMalformed(t *testing.T) {
	if _, err := ParseCoverProfile(strings.NewReader("mode: set\nnonsense\n")); err == nil {
		t.Error("expected error")
	}
}
