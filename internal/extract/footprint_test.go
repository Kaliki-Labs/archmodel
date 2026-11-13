package extract

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/model"
	"github.com/Kaliki-Labs/archmodel/probe"
)

func TestFootprintNPlusOneFixture(t *testing.T) {
	cfg, err := config.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(Options{Root: filepath.Join("../../testdata/synthetic/n-plus-one"), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		estimate string
		queries  []string
		codes    []string
	}
	cases := map[string]want{
		// a query in a range loop over the result of another: 1 + N
		"ListOrders": {"1 + N", []string{"ListOrderRows", "GetCustomer"}, []string{"n-plus-one"}},
		// a helper method and a query on the store are both followed
		"GetOrder": {"2", []string{"GetOrderRow", "GetCustomer"}, nil},
		// "for i := 0; i < 3" is a fixed 3, not N
		"RecentOrders": {"4", []string{"ListOrderRows", "ListCustomersByIDs"}, nil},
		// store.OrderSummary shares its name with the handler
		"OrderSummary": {"1", []string{"CountOrders"}, nil},
	}
	for _, op := range m.Operations {
		w, ok := cases[op.ID]
		if !ok {
			t.Fatalf("unexpected operation %s", op.ID)
		}
		if op.SQL == nil {
			t.Fatalf("%s: no footprint", op.ID)
		}
		if op.SQL.Estimate != w.estimate {
			t.Errorf("%s: estimate %q, want %q", op.ID, op.SQL.Estimate, w.estimate)
		}
		var qs, codes []string
		for _, q := range op.SQL.Queries {
			qs = append(qs, q.Query)
		}
		for _, f := range op.SQL.Findings {
			codes = append(codes, f.Code)
			if f.Rationale == "" || f.Fix == "" {
				t.Errorf("%s/%s: finding without rationale or fix", op.ID, f.Code)
			}
		}
		if !reflect.DeepEqual(qs, w.queries) {
			t.Errorf("%s: queries %v, want %v", op.ID, qs, w.queries)
		}
		if !reflect.DeepEqual(codes, w.codes) {
			t.Errorf("%s: findings %v, want %v", op.ID, codes, w.codes)
		}
		delete(cases, op.ID)
	}
	if len(cases) != 0 {
		t.Errorf("operations missing from the model: %v", cases)
	}
}

func TestTooManyQueriesIsAnUpperBoundWarning(t *testing.T) {
	b := &builder{cfg: &config.Config{SQL: config.SQLConfig{MaxQueriesPerOperation: 2}}}
	qs := []model.OperationQuery{{Query: "A", Times: 1}, {Query: "B", Times: 1}, {Query: "C", Times: 1}}
	s := b.summarizeFootprint(&model.Operation{ID: "X"}, qs)
	if s.Estimate != "3" || len(s.Findings) != 1 || s.Findings[0].Code != "too-many-queries" || s.Findings[0].Severity != "info" {
		t.Errorf("got %+v", s)
	}
	b.cfg.SQL.MaxQueriesPerOperation = 0
	if s := b.summarizeFootprint(&model.Operation{ID: "X"}, qs); len(s.Findings) != 0 {
		t.Errorf("limit 0 should disable the check, got %+v", s.Findings)
	}
}

func TestClassifyProbe(t *testing.T) {
	cases := []struct {
		name   string
		runs   []probe.Run
		growth string
	}{
		{"constant", []probe.Run{{Rows: 5, Queries: 3}, {Rows: 50, Queries: 3}}, "constant"},
		{"one extra count query", []probe.Run{{Rows: 5, Queries: 6}, {Rows: 50, Queries: 7}}, "constant"},
		{"fewer queries", []probe.Run{{Rows: 5, Queries: 9}, {Rows: 50, Queries: 6}}, "constant"},
		{"linear", []probe.Run{{Rows: 5, Queries: 6}, {Rows: 50, Queries: 51}}, "linear"},
		{"linear three", []probe.Run{{Rows: 10, Queries: 11}, {Rows: 20, Queries: 21}, {Rows: 40, Queries: 41}}, "linear"},
		{"superlinear", []probe.Run{{Rows: 10, Queries: 10}, {Rows: 20, Queries: 40}, {Rows: 40, Queries: 160}}, "superlinear"},
	}
	for _, c := range cases {
		g, _, ok := classifyProbe(c.runs)
		if !ok || g != c.growth {
			t.Errorf("%s: got %q ok=%v, want %q", c.name, g, ok, c.growth)
		}
	}
	if _, _, ok := classifyProbe([]probe.Run{{Rows: 5, Queries: 1}, {Rows: 5, Queries: 2}}); ok {
		t.Error("same size twice should not classify")
	}
}

func TestReconcileProbe(t *testing.T) {
	static := []model.Finding{{Code: "n-plus-one", Severity: "warn", Message: "x"}}
	lin := &model.Probe{Growth: "linear", Slope: 1, Runs: []model.ProbeRun{{Rows: 5, Queries: 6}, {Rows: 50, Queries: 51}}}
	if got := reconcileProbe(static, lin); len(got) != 1 || !strings.Contains(got[0].Message, "confirmed by probe") {
		t.Errorf("confirm: %+v", got)
	}
	flat := &model.Probe{Growth: "constant", Runs: []model.ProbeRun{{Rows: 5, Queries: 3}, {Rows: 50, Queries: 3}}}
	if got := reconcileProbe(static, flat); len(got) != 1 || got[0].Code != "n-plus-one-not-observed" {
		t.Errorf("not observed: %+v", got)
	}
	if got := reconcileProbe(nil, lin); len(got) != 1 || got[0].Code != "n-plus-one" {
		t.Errorf("probe-only: %+v", got)
	}
}
