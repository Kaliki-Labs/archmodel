// Package probe measures how many database queries an API operation runs as
// its data grows, so a pull request can show a confirmed N+1 instead of a
// suspected one (ADR-0013).
//
// It is meant for a consumer's own Go test, which already has a database, a
// router and fixtures: seed a few rows, call the handler, count the queries,
// seed more, call again. If the count grows with the rows, the handler runs a
// query per row.
//
//	counter := &probe.Counter{}
//	// Feed it from the driver: for pgx, a QueryTracer whose TraceQueryStart
//	// calls counter.Record(data.SQL).
//	rec := probe.NewRecorder()
//	err := rec.Measure("ListOrders", counter, []int{5, 50},
//		func(n int) error { return seedOrders(n) },      // make n rows exist
//		func() error { return callListOrders(router) })   // one request
//	...
//	rec.WriteFile(".archmodel/probe.json")
//
// then: archmodel build --probe .archmodel/probe.json
//
// The package has no dependencies, so it adds nothing to a consumer's module
// beyond archmodel itself.
package probe

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// Version is the probe file format version.
const Version = 1

// maxStatements bounds how many distinct statements a run records.
const maxStatements = 20

// Counter counts the statements sent to the database. Wire Record into the
// driver's tracing hook; the counter is safe for concurrent use.
type Counter struct {
	mu    sync.Mutex
	n     int
	stmts []string
	seen  map[string]bool
}

// Record notes one statement.
func (c *Counter) Record(sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	s := strings.Join(strings.Fields(sql), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if !c.seen[s] && len(c.stmts) < maxStatements {
		c.seen[s] = true
		c.stmts = append(c.stmts, s)
	}
}

// Reset clears the count and the recorded statements.
func (c *Counter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n, c.stmts, c.seen = 0, nil, nil
}

// Count is the number of statements recorded since the last Reset.
func (c *Counter) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func (c *Counter) statements() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.stmts...)
}

// Run is one measured call.
type Run struct {
	Rows       int      `json:"rows"`
	Queries    int      `json:"queries"`
	Statements []string `json:"statements,omitempty"`
}

// Operation holds the runs measured for one operation.
type Operation struct {
	Runs []Run `json:"runs"`
}

// File is the probe file archmodel reads.
type File struct {
	Version    int                  `json:"version"`
	Operations map[string]Operation `json:"operations"`
}

// Recorder collects measurements and writes the probe file.
type Recorder struct {
	mu   sync.Mutex
	file File
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{file: File{Version: Version, Operations: map[string]Operation{}}}
}

// Measure runs an operation once per size. For each size it calls seed(size)
// (make that many rows exist; it must be cumulative-safe, so seeding 50 after
// 5 may add 45 or reset and add 50), resets the counter, calls call once, and
// records how many statements ran. sizes need at least two distinct values to
// tell constant from growing; three show whether the growth is linear.
func (r *Recorder) Measure(operationID string, c *Counter, sizes []int, seed func(rows int) error, call func() error) error {
	if operationID == "" || c == nil || seed == nil || call == nil {
		return fmt.Errorf("probe: Measure needs an operation id, a counter, seed and call")
	}
	if len(sizes) < 2 {
		return fmt.Errorf("probe: %s: need at least two sizes", operationID)
	}
	op := Operation{}
	for _, n := range sizes {
		if err := seed(n); err != nil {
			return fmt.Errorf("probe: %s: seeding %d rows: %w", operationID, n, err)
		}
		c.Reset()
		if err := call(); err != nil {
			return fmt.Errorf("probe: %s: calling with %d rows: %w", operationID, n, err)
		}
		op.Runs = append(op.Runs, Run{Rows: n, Queries: c.Count(), Statements: c.statements()})
	}
	r.mu.Lock()
	r.file.Operations[operationID] = op
	r.mu.Unlock()
	return nil
}

// File returns the collected measurements.
func (r *Recorder) File() File {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := File{Version: Version, Operations: map[string]Operation{}}
	for k, v := range r.file.Operations {
		f.Operations[k] = v
	}
	return f
}

// WriteFile writes the probe file as JSON, creating the directory if needed.
func (r *Recorder) WriteFile(path string) error {
	data, err := json.MarshalIndent(r.File(), "", "  ")
	if err != nil {
		return err
	}
	if i := strings.LastIndexAny(path, `/\`); i > 0 {
		if err := os.MkdirAll(path[:i], 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Operations lists the measured operation ids, sorted.
func (f File) OperationIDs() []string {
	out := make([]string, 0, len(f.Operations))
	for id := range f.Operations {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
