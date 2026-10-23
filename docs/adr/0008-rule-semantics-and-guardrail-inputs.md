---
status: accepted
---

# Rule semantics, embedded violations and guardrail inputs

The brief names five rules and says `check` and `query` exist. It leaves open
what exactly counts as a violation, where results live, and what a command
reads.

## Decision

**Semantics.** Rules 1-4 read the **production** dependency graph only: an
edge only a `_test.go` file creates never violates, and a Test-only module has
no Level and its own imports are outside the graph.

1. *Inward only*: an edge from Level `a` to Level `b` violates when `b` is
   outward of `a` (`b < a` in the Level order). Same-Level edges are not rule
   1's concern: Domain to Domain is rule 2's job, and Composition root to
   Composition root is allowed (`cmd/server` to `server`, `server` to `migrate`).
2. *No Domain to Domain*: any production edge between two Domains.
3. *Nothing imports the Composition root*: a production edge from any
   non-Composition-root module to a Composition root. `testsupport` to
   `migrate` is fine, since `testsupport` is Test-only. An edge can break rules 1
   and 3 at once (a Domain importing `server`); both are reported, because
   each teaches something different.
4. *Test-only out of prod*: a production (non-`_test.go`) import of a
   Test-only module. Detection (ADR-0007) says a module with a production
   importer is not Test-only, so by detection alone this cannot fire. It fires
   for modules declared Test-only through `kinds.overrides`. That is
   intentional: detection never hides a real production import by guessing, and
   an explicit declaration is enforced.
5. *Package doc comment*: every Module that is `present` and not `generated`
   needs one. Missing packages are skipped (ADR-0003); generated ones are too,
   since nobody maintains their comment (sqlc output has no package doc).

**Results live in the model.** Rules, violations (rule, severity, offending
edge or module, Rationale, fix hint, import `file:line` sites) and the metric
Rationales are embedded as additive `rules`, `violations` and
`metric_rationales` keys, and each import edge gains `sites`. The Explorer and
the diff read them from the model alone. Each Module's metrics also carry a
plain-language `reading`, produced in Go (`internal/metrics`), so the teaching
text is not duplicated in the UI. Metric Rationales are configurable like rule
Rationales (`metric_rationales:` in config).

**Inputs.** `check` and `query` build the model fresh by default, with the same
flags as `build` (`--root`, `--config`, `--coverprofile`), so an agent never
reads a stale file. `--model <path>` reads an existing `model.json` instead.

**Exit codes.** `check`: 0 with no error-severity violation (warnings print but
pass), 1 with any, 2 for usage errors. `query`: an unknown name prints a JSON
`{"error", "did_you_mean"}` and exits 1.

`thresholds:` stays reserved in config and ignored in v1. Rule severity must be
`error` or `warn`.

## Consequences

A model built before this change has no `rules`; `--model` refuses it with a
message to rebuild. Violations are recomputed on every build, so they can never
disagree with the edges beside them.
