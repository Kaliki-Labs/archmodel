package extract

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

const scoreSchema = `-- +goose Up
CREATE TABLE users (
    id    UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name  TEXT NOT NULL,
    role  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_users_role_created ON users (role, created_at DESC);
CREATE TABLE jobs (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    done_at TIMESTAMPTZ
);
CREATE INDEX idx_jobs_pending ON jobs (created_at) WHERE done_at IS NULL;
`

func TestCatalogIndexes(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("m.sql", scoreSchema)
	var users, jobs model.Table
	for _, tb := range c.Tables() {
		switch tb.Name {
		case "users":
			users = tb
		case "jobs":
			jobs = tb
		}
	}
	wantUsers := []model.Index{
		{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Primary: true},
		{Name: "users_email_key", Columns: []string{"email"}, Unique: true},
		{Name: "idx_users_role_created", Columns: []string{"role", "created_at"}},
	}
	if !reflect.DeepEqual(users.Indexes, wantUsers) {
		t.Errorf("users indexes:\n got %+v\nwant %+v", users.Indexes, wantUsers)
	}
	if got := jobs.Indexes[1]; got.Where != "done_at is null" || got.Columns[0] != "created_at" {
		t.Errorf("partial index: %+v", got)
	}
}

func TestDropIndex(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("m.sql", scoreSchema+"DROP INDEX idx_users_role_created;\n")
	for _, tb := range c.Tables() {
		for _, ix := range tb.Indexes {
			if ix.Name == "idx_users_role_created" {
				t.Errorf("index survived DROP INDEX")
			}
		}
	}
}

func TestScoreQuery(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("m.sql", scoreSchema)
	tables := map[string]model.Table{}
	for _, tb := range c.Tables() {
		tables[tb.Name] = tb
	}
	src := `-- name: GetByEmail :one
SELECT id, name FROM users WHERE email = $1;

-- name: GetByID :one
SELECT id FROM users WHERE id = $1;

-- name: ByRole :many
SELECT id FROM users WHERE role = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ByName :many
SELECT id FROM users WHERE name = $1 LIMIT 10;

-- name: Pending :many
SELECT id FROM jobs WHERE done_at IS NULL ORDER BY created_at LIMIT $1;

-- name: Everything :many
SELECT * FROM users ORDER BY name;

-- name: Search :many
SELECT id FROM users WHERE name LIKE '%' || $1 || '%' LIMIT 20 OFFSET $2;

-- name: LowerEmail :one
SELECT id FROM users WHERE lower(email) = $1;

-- name: Wipe :exec
DELETE FROM users;

-- name: Between :many
SELECT id FROM users WHERE role = $1 AND created_at BETWEEN $2 AND $3 LIMIT 5;
`
	cases := map[string]struct {
		min, max int
		codes    []string
	}{
		"GetByEmail": {100, 100, nil},
		"GetByID":    {100, 100, nil},
		"ByRole":     {100, 100, nil}, // equality prefix + index order serves the sort
		"ByName":     {70, 70, []string{"unindexed-filter"}},
		"Pending":    {100, 100, nil}, // partial index predicate stated by the query
		"Everything": {55, 65, []string{"select-star", "unbounded-list", "unindexed-sort"}},
		"Search":     {40, 60, []string{"leading-wildcard", "offset-pagination", "unindexed-filter"}},
		"LowerEmail": {75, 75, []string{"function-on-column"}},
		"Wipe":       {55, 60, []string{"unbounded-write"}},
		"Between":    {100, 100, nil},
	}
	seen := 0
	for _, q := range ParseQueries(src, func(n string) bool { _, ok := tables[n]; return ok }) {
		want, ok := cases[q.Name]
		if !ok {
			t.Fatalf("unexpected query %s", q.Name)
		}
		seen++
		got := ScoreQuery(q, tables)
		var codes []string
		for _, f := range got.Findings {
			codes = append(codes, f.Code)
			if f.Rationale == "" || f.Fix == "" || f.Message == "" {
				t.Errorf("%s/%s: finding missing rationale, fix or message: %+v", q.Name, f.Code, f)
			}
		}
		sort.Strings(codes)
		w := append([]string(nil), want.codes...)
		sort.Strings(w)
		if len(codes) != len(w) || (len(w) > 0 && !reflect.DeepEqual(codes, w)) {
			t.Errorf("%s: codes %v, want %v", q.Name, codes, w)
		}
		if got.Value < want.min || got.Value > want.max {
			t.Errorf("%s: score %d, want %d-%d", q.Name, got.Value, want.min, want.max)
		}
	}
	if seen != len(cases) {
		t.Errorf("parsed %d queries, want %d", seen, len(cases))
	}
}

// Patterns taken from real kaliki projects (deklutr, instituteos) that an
// earlier version of the scorer got wrong.
func TestScoreQueryRealWorldPatterns(t *testing.T) {
	c := NewCatalog()
	c.ApplyMigration("m.sql", `-- +goose Up
CREATE TABLE tasks (id UUID PRIMARY KEY, user_id UUID NOT NULL, page_id UUID, trashed_at TIMESTAMPTZ, due_date DATE, rank TEXT);
CREATE INDEX idx_tasks_container ON tasks (user_id, page_id, rank) WHERE trashed_at IS NULL;
CREATE INDEX idx_tasks_due ON tasks (user_id, due_date) WHERE trashed_at IS NULL AND due_date IS NOT NULL;
CREATE TABLE subtasks (id UUID PRIMARY KEY, task_id UUID NOT NULL, position INT);
CREATE INDEX idx_subtasks_task ON subtasks (task_id, position);
CREATE TABLE notes (id UUID PRIMARY KEY, body TEXT);
`)
	tables := map[string]model.Table{}
	for _, tb := range c.Tables() {
		tables[tb.Name] = tb
	}
	src := `-- name: FromView :many
SELECT s.* FROM subtask_details s JOIN tasks t ON t.id = s.task_id AND t.trashed_at IS NULL
WHERE s.task_id = @task_id ORDER BY s.position LIMIT 100;

-- name: PartialInJoin :many
SELECT t.id FROM tasks t
WHERE t.user_id = @u AND t.trashed_at IS NULL ORDER BY t.page_id LIMIT 50;

-- name: DueRange :many
SELECT id FROM tasks WHERE user_id = @u AND due_date <= @d AND trashed_at IS NULL ORDER BY due_date LIMIT 50;

-- name: CaseFilter :many
SELECT id FROM tasks
WHERE CASE WHEN @p::uuid IS NULL THEN page_id IS NULL AND user_id = @u ELSE page_id = @p END
ORDER BY rank LIMIT 10;

-- name: InsertReturning :one
INSERT INTO notes (body) VALUES ($1) RETURNING *;

-- name: CountAll :many
SELECT count(*) FROM tasks WHERE user_id = @u AND trashed_at IS NULL;

-- name: ByPK :many
SELECT id, body FROM notes WHERE id = ANY(@ids);

-- name: CTELimit :many
WITH due AS (
  SELECT id FROM tasks WHERE user_id = @u AND trashed_at IS NULL ORDER BY rank LIMIT 5
)
SELECT t.id FROM tasks t JOIN due d ON d.id = t.id WHERE t.user_id = @u AND t.trashed_at IS NULL LIMIT 5;

-- name: SubqueryLimitDoesNotCount :many
SELECT id FROM notes WHERE id IN (SELECT id FROM notes LIMIT 3);
`
	want := map[string][]string{
		"FromView":                  {"select-star"},
		"PartialInJoin":             nil,
		"DueRange":                  nil,
		"CaseFilter":                nil,
		"InsertReturning":           nil,
		"CountAll":                  nil,
		"ByPK":                      nil,                // IN-list: PK is the leading index column; not a scan
		"CTELimit":                  {"unindexed-sort"}, // rank is not next in (user_id, page_id, rank)
		"SubqueryLimitDoesNotCount": {"unbounded-list"},
	}
	seen := 0
	for _, q := range ParseQueries(src, func(n string) bool { _, ok := tables[n]; return ok }) {
		seen++
		var codes []string
		for _, f := range ScoreQuery(q, tables).Findings {
			codes = append(codes, f.Code)
		}
		if len(codes) != len(want[q.Name]) || (len(codes) > 0 && !reflect.DeepEqual(codes, want[q.Name])) {
			t.Errorf("%s: findings %v, want %v", q.Name, codes, want[q.Name])
		}
	}
	if seen != len(want) {
		t.Errorf("parsed %d queries, want %d", seen, len(want))
	}
}
