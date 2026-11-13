package probe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fakeDB stands in for a database: it holds n rows and counts statements.
type fakeDB struct {
	rows int
	c    *Counter
}

func (d *fakeDB) listOrders(nPlusOne bool) {
	d.c.Record("SELECT id, customer_id FROM orders LIMIT 50")
	if nPlusOne {
		for i := 0; i < d.rows; i++ {
			d.c.Record("SELECT id, name FROM customers WHERE id = $1")
		}
		return
	}
	d.c.Record("SELECT id, name FROM customers WHERE id = ANY($1)")
}

func TestMeasureCountsGrowth(t *testing.T) {
	c := &Counter{}
	db := &fakeDB{c: c}
	rec := NewRecorder()
	seed := func(n int) error { db.rows = n; return nil }
	if err := rec.Measure("ListOrders", c, []int{5, 50}, seed, func() error { db.listOrders(true); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := rec.Measure("ListOrdersBatched", c, []int{5, 50}, seed, func() error { db.listOrders(false); return nil }); err != nil {
		t.Fatal(err)
	}
	f := rec.File()
	bad := f.Operations["ListOrders"].Runs
	if len(bad) != 2 || bad[0].Queries != 6 || bad[1].Queries != 51 {
		t.Errorf("n+1 runs: %+v", bad)
	}
	if len(bad[1].Statements) != 2 {
		t.Errorf("distinct statements should be deduped: %v", bad[1].Statements)
	}
	good := f.Operations["ListOrdersBatched"].Runs
	if good[0].Queries != 2 || good[1].Queries != 2 {
		t.Errorf("batched runs: %+v", good)
	}
}

func TestMeasureRejectsBadInput(t *testing.T) {
	c := &Counter{}
	rec := NewRecorder()
	ok := func(int) error { return nil }
	if rec.Measure("X", c, []int{5}, ok, func() error { return nil }) == nil {
		t.Error("one size should be rejected")
	}
	if rec.Measure("", c, []int{1, 2}, ok, func() error { return nil }) == nil {
		t.Error("empty id should be rejected")
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	c := &Counter{}
	rec := NewRecorder()
	_ = rec.Measure("Op", c, []int{1, 2}, func(int) error { return nil }, func() error { c.Record("SELECT 1"); return nil })
	path := filepath.Join(t.TempDir(), "sub", "probe.json")
	if err := rec.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var f File
	if err := json.Unmarshal(data, &f); err != nil || f.Version != Version || len(f.Operations["Op"].Runs) != 2 {
		t.Errorf("round trip: %v %+v", err, f)
	}
}
