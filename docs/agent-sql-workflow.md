# SQL review: archmodel for agents

Paste the section below into a project's `CLAUDE.md`. archmodel reports; the
agent decides. Background: [ADR-0012](adr/0012-static-sql-query-scoring.md),
[ADR-0013](adr/0013-operation-query-footprint-and-probe.md).

---

## SQL review (archmodel)

Run `archmodel query sql-attention` after changing SQL or a handler. It lists
queries scoring below the threshold and operations with N+1 or
too-many-queries findings, worst first. Work one item at a time:

1. `archmodel query sql-query <Name>`: the score, findings with fixes, the
   table's indexes, and every operation that reaches the query (`reached_by`).
2. Change only that query (or that call site). Prefer a fix in the finding's
   `fix` field. Add the index in a new migration, never edit an applied one.
3. Rebuild and re-run `sql-attention`. The item must leave the list. A rewrite
   that changes shape (a JOIN, a batch `ANY($1)`) must return the same rows:
   say how you checked.

**The static footprint is a high-level estimate. Know where it is blind**
(`sql-operations` and `sql-attention` repeat these in `limits`):

- *Shows 0 or few queries but the handler delegates elsewhere* (another Module,
  an interface, a callback): read the handler and follow the calls yourself.
- *Flags N+1 on a loop*: open the call site. A small fixed list, a cache-miss
  path or a one-element slice is not a problem. If it matters, write a probe.
- *Estimate counts every branch*: it is an upper bound.

**Probe (confirm an N+1 with data).** In an integration test with a database:
seed 5 then 50 rows, call the handler, count statements with
`probe.Counter` (feed `Record` from the pgx tracer), `Recorder.Measure(opID,
...)`, `WriteFile(".archmodel/probe.json")`, then
`archmodel build --probe .archmodel/probe.json`. `growth: linear` confirms it;
`constant` clears it. Only probe operations you could not settle by reading.
