package diff

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func sqlModel(score int, findings []model.Finding, est string, perItem int, opFindings []model.Finding) *model.Model {
	return &model.Model{
		Version: model.Version,
		SQL:     &model.SQLPolicy{ScoreThreshold: 75, MaxQueriesPerOperation: 8},
		Queries: []model.Query{{ID: "ListOrders", Module: "orders", Score: &model.QueryScore{Value: score, Grade: "B", Findings: findings}}},
		Operations: []model.Operation{{
			ID: "ListOrders", Method: "GET", Path: "/orders",
			SQL: &model.OperationSQL{Estimate: est, Fixed: 1, PerItem: perItem, Findings: opFindings},
		}},
	}
}

func TestDiffSQL(t *testing.T) {
	npo := []model.Finding{{Code: "n-plus-one", Severity: "warn"}}
	unb := []model.Finding{{Code: "unbounded-list", Severity: "warn", Fix: "add LIMIT"}}
	base := sqlModel(100, nil, "1", 0, nil)
	head := sqlModel(80, unb, "1 + N", 1, npo)
	d := Compute(base, head)
	if !d.Changed || len(d.SQL.Queries) != 1 || d.SQL.Queries[0].Status != "worse" || d.SQL.Queries[0].Findings[0] != "unbounded-list" {
		t.Fatalf("queries: %+v", d.SQL.Queries)
	}
	if len(d.SQL.Operations) != 1 || d.SQL.Operations[0].Status != "worse" || d.SQL.Operations[0].To != "1 + N" {
		t.Fatalf("operations: %+v", d.SQL.Operations)
	}
	md := d.Markdown(MarkdownOptions{})
	for _, want := range []string{"**SQL**", "1 → 1 + N", "n-plus-one", "score 100 → 80", "2 SQL changes"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	// The reverse is an improvement, listed last.
	r := Compute(head, base)
	if r.SQL.Queries[0].Status != "better" || r.SQL.Operations[0].Status != "better" {
		t.Errorf("reverse: %+v %+v", r.SQL.Queries, r.SQL.Operations)
	}
	// No change, no SQL section.
	if same := Compute(base, sqlModel(100, nil, "1", 0, nil)); same.Changed {
		t.Errorf("identical models changed: %+v", same.SQL)
	}
}

func TestDiffSQLAddedQueryAndColumns(t *testing.T) {
	mk := func(uses []model.OperationQuery, cols []string, hash string) *model.Model {
		fixed := 0
		for _, u := range uses {
			if !u.InLoop {
				fixed++
			}
		}
		return &model.Model{
			Version: model.Version,
			SQL:     &model.SQLPolicy{ScoreThreshold: 75},
			Queries: []model.Query{{ID: "GetOrder", Module: "orders", Hash: hash, Columns: cols, ColumnsFrom: "generated",
				Score: &model.QueryScore{Value: 95, Grade: "A", Findings: []model.Finding{}}}},
			Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/{id}",
				SQL: &model.OperationSQL{Queries: uses, Fixed: fixed, Estimate: fmt.Sprint(fixed), Findings: []model.Finding{}}}},
		}
	}
	use := func(q string) model.OperationQuery {
		return model.OperationQuery{Query: q, Times: 1, Via: []string{"GetOrder", "load"}}
	}
	base := mk([]model.OperationQuery{use("GetOrder"), use("GetCustomer"), use("ListLines")}, []string{"id", "total"}, "a")
	head := mk([]model.OperationQuery{use("GetOrder"), use("GetCustomer"), use("ListLines"), use("GetShipment")}, []string{"id", "total", "internal_notes"}, "b")
	d := Compute(base, head)
	if len(d.SQL.Operations) != 1 || len(d.SQL.Operations[0].Added) != 1 || d.SQL.Operations[0].Added[0].Query != "GetShipment" {
		t.Fatalf("operations: %+v", d.SQL.Operations)
	}
	if len(d.SQL.Queries) != 1 || d.SQL.Queries[0].ColumnsAdded[0] != "internal_notes" || len(d.SQL.Queries[0].ReachedBy) != 1 {
		t.Fatalf("queries: %+v", d.SQL.Queries)
	}
	md := d.Markdown(MarkdownOptions{})
	for _, want := range []string{"3 → 4: + `GetShipment`", "now returns +internal_notes (from sqlc's generated code)", "Reached by `GET /orders/{id}`"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	// A reformat alone (same hash) is not a change.
	if same := Compute(base, mk(base.Operations[0].SQL.Queries, []string{"id", "total"}, "a")); same.Changed {
		t.Errorf("identical SQL changed: %+v", same.SQL)
	}
}
