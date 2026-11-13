package extract

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func tbl(name string, cols ...string) model.Table {
	t := model.Table{Name: name}
	for _, c := range cols {
		t.Columns = append(t.Columns, model.Column{Name: c})
	}
	return t
}

func TestOutputColumns(t *testing.T) {
	tables := map[string]model.Table{"orders": tbl("orders", "id", "total", "note"), "customers": tbl("customers", "id", "name")}
	cases := []struct {
		name, sql, from string
		want            []string
	}{
		{"star", "SELECT * FROM orders WHERE id = $1", colsSchema, []string{"id", "total", "note"}},
		{"listed", "SELECT id, total FROM orders", colsText, []string{"id", "total"}},
		{"qualified star", "SELECT o.*, c.name FROM orders o JOIN customers c ON c.id = o.customer_id", colsSchema, []string{"id", "total", "note", "name"}},
		{"alias", "SELECT count(*) AS n, o.total AS t FROM orders o", colsText, []string{"n", "t"}},
		{"returning star", "INSERT INTO orders (total) VALUES ($1) RETURNING *", colsSchema, []string{"id", "total", "note"}},
		{"update returning", "UPDATE orders SET total = $1 WHERE id = $2 RETURNING id, total", colsText, []string{"id", "total"}},
		{"exec", "DELETE FROM orders WHERE id = $1", "", nil},
	}
	for _, c := range cases {
		got, from := outputColumns(c.sql, tables)
		if !reflect.DeepEqual(got, c.want) || from != c.from {
			t.Errorf("%s: got %v (%s), want %v (%s)", c.name, got, from, c.want, c.from)
		}
	}
}

func TestGeneratedSQLAndHash(t *testing.T) {
	dir := t.TempDir()
	src := "package db\n\nconst getOrder = `-- name: GetOrder :one\nSELECT id, total, note FROM orders WHERE id = $1\n`\n"
	if err := os.WriteFile(filepath.Join(dir, "orders.sql.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := generatedSQL(dir)
	cols, _ := outputColumns(gen["GetOrder"], map[string]model.Table{})
	if !reflect.DeepEqual(cols, []string{"id", "total", "note"}) {
		t.Errorf("generated columns: %v (%v)", cols, gen)
	}
	if generatedSQL(t.TempDir()) != nil {
		t.Error("no generated code should give nil")
	}
	if textHash("SELECT  a\nFROM t") != textHash("select a from t") || textHash("select a from t") == textHash("select b from t") {
		t.Error("hash should ignore whitespace and case only")
	}
}
