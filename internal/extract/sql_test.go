package extract

import (
	"reflect"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func col(c model.Column) model.Column { return c }

func TestCreateTableColumns(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("m1.sql", `-- +goose Up
-- a comment; with a semicolon
CREATE TABLE IF NOT EXISTS public.widgets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(255) NOT NULL DEFAULT 'a, b',
    price      NUMERIC(10,2),
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    at         TIMESTAMP WITH TIME ZONE NOT NULL,
    tags       TEXT[] NOT NULL DEFAULT '{}',
    "Quoted"   INT, -- trailing comment
    CONSTRAINT widgets_name UNIQUE (name)
);
CREATE INDEX idx ON widgets (name);
-- +goose Down
DROP TABLE widgets;
`)
	tabs := c.Tables()
	if len(tabs) != 1 || tabs[0].Name != "widgets" {
		t.Fatalf("tables: %+v", tabs)
	}
	want := []model.Column{
		{Name: "id", Type: "uuid", PrimaryKey: true},
		{Name: "name", Type: "varchar(255)"},
		{Name: "price", Type: "numeric(10,2)", Nullable: true},
		{Name: "owner_id", Type: "uuid", References: "users"},
		{Name: "at", Type: "timestamp with time zone"},
		{Name: "tags", Type: "text[]"},
		{Name: "Quoted", Type: "int", Nullable: true},
	}
	if !reflect.DeepEqual(tabs[0].Columns, want) {
		t.Errorf("columns:\n got %+v\nwant %+v", tabs[0].Columns, want)
	}
	if !reflect.DeepEqual(tabs[0].Migrations, []string{"m1.sql"}) {
		t.Errorf("migrations: %v", tabs[0].Migrations)
	}
}

func TestOnlyUpSectionApplies(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("m.sql", "-- +goose Up\nCREATE TABLE a (id INT);\n-- +goose Down\nCREATE TABLE b (id INT);\nDROP TABLE a;\n")
	if !c.Has("a") || c.Has("b") {
		t.Errorf("tables: %+v", c.Tables())
	}
}

func TestAlterAndDrop(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("1.sql", "-- +goose Up\nCREATE TABLE t (id INT PRIMARY KEY, a TEXT, b TEXT);\nCREATE TABLE gone (id INT);\n")
	c.ApplyMigration("2.sql", `-- +goose Up
ALTER TABLE t ADD COLUMN c INT NOT NULL DEFAULT 0, ADD COLUMN IF NOT EXISTS d TEXT;
ALTER TABLE t DROP COLUMN a;
ALTER TABLE t RENAME COLUMN b TO bee;
ALTER TABLE t ALTER COLUMN d SET NOT NULL;
ALTER TABLE t ALTER COLUMN c TYPE BIGINT;
ALTER TABLE t ADD CONSTRAINT fk FOREIGN KEY (c) REFERENCES other(id);
DROP TABLE IF EXISTS gone CASCADE;
-- +goose Down
ALTER TABLE t DROP COLUMN c;
`)
	if c.Has("gone") {
		t.Error("gone should be dropped")
	}
	tab := c.Tables()[0]
	want := []model.Column{
		{Name: "id", Type: "int", PrimaryKey: true},
		{Name: "bee", Type: "text", Nullable: true},
		{Name: "c", Type: "bigint", References: "other"},
		{Name: "d", Type: "text"},
	}
	if !reflect.DeepEqual(tab.Columns, want) {
		t.Errorf("columns:\n got %+v\nwant %+v", tab.Columns, want)
	}
	if !reflect.DeepEqual(tab.Migrations, []string{"1.sql", "2.sql"}) {
		t.Errorf("migrations: %v", tab.Migrations)
	}
}

func TestDollarQuotedFunctionDoesNotBreakSplitting(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("1.sql", `-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION f() RETURNS trigger AS $$
BEGIN
  NEW.x = 1; RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TABLE after_fn (id INT);
`)
	if !c.Has("after_fn") {
		t.Error("table after a function body was lost")
	}
}

func TestParseQueries(t *testing.T) {
	src := `-- name: ListThings :many
SELECT t.id FROM things t
JOIN owners o ON o.id = t.owner_id
WHERE t.x IN (SELECT id FROM cte_like);

-- name: Mark :exec
UPDATE things SET a = 1 WHERE id = $1;

-- name: Enqueue :one
INSERT INTO outbox (topic) VALUES ($1) RETURNING *;

-- name: Purge :execrows
DELETE FROM things WHERE gone; -- also touches nothing else
`
	known := func(n string) bool { return n == "things" || n == "owners" || n == "outbox" }
	qs := ParseQueries(src, known)
	if len(qs) != 4 {
		t.Fatalf("got %d queries: %+v", len(qs), qs)
	}
	check := func(i int, name, kind string, line int, tables ...string) {
		t.Helper()
		q := qs[i]
		if q.Name != name || q.Kind != kind || q.Line != line || !reflect.DeepEqual(q.Tables, tables) {
			t.Errorf("query %d = %+v, want %s :%s line %d %v", i, q, name, kind, line, tables)
		}
	}
	check(0, "ListThings", "many", 1, "owners", "things")
	check(1, "Mark", "exec", 6, "things")
	check(2, "Enqueue", "one", 9, "outbox")
	check(3, "Purge", "execrows", 12, "things")
}
