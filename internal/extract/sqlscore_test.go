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
