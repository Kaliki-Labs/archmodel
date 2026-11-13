---
status: proposed
---

# Static SQL query scoring

Deklutr task "SQL optimization query scoring": can SQL optimization information
live in the Architecture model? This is a proof of concept answering yes.

## Decision

**Static, not measured.** Each sqlc query gets a `score` (0-100, grade A-D) and a
list of findings, computed from the query text and the table's indexes. No
database, no `EXPLAIN`. That keeps `build` dependency-free and deterministic,
like the rest of the model. The cost: row counts are unknown, so findings say
"will not scale", not "is slow".

**Indexes join the model.** `Table.indexes` records the primary key, UNIQUE
constraints and `CREATE INDEX` (including partial indexes and `DROP INDEX`),
because a filter or sort is only good or bad relative to them.

**Findings carry a Rationale and a fix**, like rules: `unindexed-filter`,
`unindexed-sort`, `unbounded-list`, `unbounded-write`, `offset-pagination`,
`leading-wildcard`, `function-on-column`, `select-star`, `or-filter`. Penalties
are fixed in code.

**Displayed, not enforced**, matching package metrics (brief: no threshold gates
in v1). `archmodel query sql-score` lists queries worst first; the score is also
on `Query` in the model, so it shows in `query module`, `query table` and the
JSON an agent reads.

Both additions are new keys; `version` stays 1.

## Known limits (open)

- Joins are scored on the first table only; CTEs and subqueries are not unpacked.
- A partial index counts as usable only when the query states its predicate verbatim.
- Penalties and the grade cut-offs are not configurable yet; `sql.score_threshold` (default 75) only sets what counts as needing attention.
- Shown in the Explorer (grade badges, findings, table indexes) and in the PR comment's SQL section (score drops and new low scorers); see ADR-0013 for the per-operation layer.
- A real `EXPLAIN` mode against a scratch Postgres could later confirm or refine the static findings.
