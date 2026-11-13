---
status: proposed
---

# Per-operation query footprint and runtime probe

[ADR-0012](0012-static-sql-query-scoring.md) scores each query alone. It cannot
see the common problem: a handler that runs a query per row (N+1), or runs too
many queries per request. That is a property of the *operation*, so it is
measured per operation, in two layers that agree or disagree visibly.

## Decision

**Layer 2: static footprint.** `Operation.sql` lists the queries a handler
reaches. The walker follows calls from the handler through methods and helpers
in the same Module (receiver field types resolve `s.store.X`; sqlc method names
equal query names, so no generated code is needed, ADR-0003). A call inside a
`for`/`range` is *in a loop*; a loop with a literal bound counts as a fixed
multiplier. The result is `estimate` (`"3"`, `"1 + N"`, `"3 + 2N"`) plus
findings `n-plus-one` (warn) and `too-many-queries` (info; `sql.max_queries_per_operation`,
default 8). The estimate is an **upper bound over every branch**, so it says
"up to", not "is".

**Layer 3: runtime probe.** The public, dependency-free `probe` package lets a
consumer's own test seed 5 then 50 rows, call the handler, and count statements
(feed `Counter.Record` from a pgx `QueryTracer`). `Recorder.WriteFile` writes
`probe.json`; `archmodel build --probe probe.json` merges it by operation id
into `Operation.sql.probe` (`runs`, `growth` constant|linear|superlinear,
`slope`). A probe is proof: a linear probe confirms the static finding (or
raises one the walker missed); a constant probe downgrades a static `n-plus-one`
to `n-plus-one-not-observed` (info). Probe files are inputs like a coverprofile;
`build` stays deterministic given its inputs.

**Surfaces.** Explorer operation page ("Queries per request"), `query
sql-operations`, `query sql-attention` (the work queue: queries below
`sql.score_threshold` and operations with SQL findings), and the PR comment's
**SQL** section (query score drops, new low scorers, estimate changes, new
N+1, improvements).

**What the diff says.** Per operation: the estimate change plus the query calls
added and removed (`3 → 4: + GetShipment (via load)`), so a count change names
its cause. Per query: score changes with the penalty of each finding, text edits
(`Query.hash`, whitespace and case ignored) and changes in what the query
returns (`Query.columns`), with the operations that reach it. *Returned
columns* are read from sqlc's generated Go when it is on disk (sqlc has already
expanded `*` there, so a table gaining a column shows up in every star query);
otherwise a `*` is expanded from the migrations and labelled "inferred".
`Query.columns_from` says which.

**Agent workflow lives outside archmodel.** archmodel reports; a template
`CLAUDE.md` or skill tells an agent: take the first `sql-attention` item, change
only that query or call site, rebuild, and confirm the item left the list (score
rose / estimate dropped). Rewrites that change shape (a JOIN) still need a human
or a test to confirm the result is equivalent.

## Known limits (open)

- Calls into other Modules are not followed; operations that delegate wholesale
  (OAuth, connections, event streams) show no queries. A probe covers them.
- `N` is unknown statically, goroutines and callbacks are not special-cased, and
  every branch counts.
- Cost comparison of a rewrite (`EXPLAIN` on a scratch Postgres, plus a result
  equivalence check) is designed but not built: it needs a database in CI.
- The probe counts statements, not time; it proves shape, not speed.
