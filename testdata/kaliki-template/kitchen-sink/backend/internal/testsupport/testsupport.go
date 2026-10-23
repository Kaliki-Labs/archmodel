// Package testsupport provides shared helpers for integration-style handler
// tests: a connection to the test database (docker/docker-compose-test.yaml),
// migrations, and per-test truncation. Tests register real routes on a real
// gin router and hit real endpoint paths.
//
// Each test package gets its OWN postgres schema (via search_path), so packages
// run in parallel without colliding. Pass a unique schema name from TestMain.
package testsupport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/kitchen-sink-app/backend/internal/migrate"
)

// TestDB is a per-package test database scoped to its own schema.
type TestDB struct {
	Pool   *pgxpool.Pool
	Schema string
}

// DatabaseURL is the test DB DSN. Override with TEST_DATABASE_URL in CI.
func DatabaseURL() string {
	return getenv("TEST_DATABASE_URL",
		"postgres://test_user:test_password@localhost:25432/test_db?sslmode=disable")
}

// RedisURL is the test Redis DSN (docker/docker-compose-test.yaml's redis
// service). Override with TEST_REDIS_URL in CI.
func RedisURL() string {
	return getenv("TEST_REDIS_URL", "redis://localhost:26379")
}

// Connect creates (if needed) an isolated schema, routes the pool to it, and
// applies migrations. Call once from TestMain with a package-unique schema name.
func Connect(schema string) *TestDB {
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(DatabaseURL())
	if err != nil {
		panic("parse test db url: " + err.Error())
	}
	// Every pooled connection lands in this package's schema first.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		panic("connect test db (is docker/docker-compose-test.yaml up?): " + err.Error())
	}
	// Start from a clean schema each run so migration bookkeeping is
	// deterministic (no stale goose_db_version from a previous run).
	if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema)); err != nil {
		panic("drop test schema: " + err.Error())
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		panic("create test schema: " + err.Error())
	}

	if err := migrate.Up(ctx, DatabaseURL(), schema, "../../sql/schema"); err != nil {
		panic("migrate test db: " + err.Error())
	}
	return &TestDB{Pool: pool, Schema: schema}
}

// NewRouter returns a gin engine in test mode with an /api/v1 group.
func NewRouter() (*gin.Engine, *gin.RouterGroup) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	return r, r.Group("/api/v1")
}

// Truncate clears every table in this package's schema except the migration
// bookkeeping table.
func (tdb *TestDB) Truncate(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	rows, err := tdb.Pool.Query(ctx,
		`SELECT tablename FROM pg_tables WHERE schemaname = $1 AND tablename <> 'schema_migrations'`,
		tdb.Schema)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, fmt.Sprintf("%q.%q", tdb.Schema, name))
	}
	rows.Close()
	if len(tables) == 0 {
		return
	}
	if _, err := tdb.Pool.Exec(ctx, "TRUNCATE "+strings.Join(tables, ", ")+" CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// DoJSON performs a request with an optional JSON body and returns the recorder.
func DoJSON(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return DoJSONAuth(r, method, path, body, "")
}

// DoJSONAuth is DoJSON with a Bearer token (omitted when token is empty).
func DoJSONAuth(r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
