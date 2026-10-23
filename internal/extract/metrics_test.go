package extract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestComputeMetrics(t *testing.T) {
	// Ca=1 Ce=3 -> I=0.75; 1 of 4 exported types abstract -> A=0.25; D=0.
	m := ComputeMetrics(1, 3, 4, 1)
	if m.Instability != 0.75 || m.Abstractness != 0.25 || m.Distance != 0 || !m.InstabilityDefined {
		t.Errorf("%+v", m)
	}
	// Isolated module: I defined as 0 and flagged; no exported types -> A=0; D=1.
	m = ComputeMetrics(0, 0, 0, 0)
	if m.Instability != 0 || m.InstabilityDefined || m.Abstractness != 0 || m.Distance != 1 {
		t.Errorf("isolated: %+v", m)
	}
	// Leaf (nobody depends on it, depends on 2): I=1, A=0 -> D=0.
	if m := ComputeMetrics(0, 2, 3, 0); m.Instability != 1 || m.Distance != 0 {
		t.Errorf("leaf: %+v", m)
	}
	// Stable and concrete: zone of pain.
	if m := ComputeMetrics(5, 0, 3, 0); m.Instability != 0 || m.Distance != 1 {
		t.Errorf("pain: %+v", m)
	}
}

func TestCyclomatic(t *testing.T) {
	src := `package p
func f(a, b int, ch chan int) int {
	if a > 0 && b > 0 || a < -5 { // if + && + ||
		return 1
	}
	for i := 0; i < a; i++ { // for
	}
	for range ch { // range
	}
	switch a {
	case 1, 2: // case
	case 3: // case
	default: // not counted
	}
	select {
	case <-ch: // comm
	default:
	}
	g := func() bool { return a > 1 || b > 1 } // || inside literal
	_ = g
	return 0
}`
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	fd := f.Decls[0].(*ast.FuncDecl)
	got := Cyclomatic(fd.Body)
	// 1 + if + && + || + for + range + 2 cases + comm + || in literal = 10
	if got != 10 {
		t.Errorf("cyclomatic = %d, want 10", got)
	}
}
