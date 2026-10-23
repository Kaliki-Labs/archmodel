---
status: accepted
---

# Architecture diff: what changed, what is notable, what is drawn

## Decision

**What counts as changed** (`internal/diff`):

- Modules added or removed; kind changed (e.g. Infrastructure to Domain).
- Dependencies added or removed, keyed by `from>to`; an edge that flips between
  production and test-only is "changed".
- Operations keyed by operationId: added, removed, or changed in route, owner,
  security, rate limit, set of response codes, response or request schemas.
  Descriptions and summaries are ignored.
- Tables keyed by name: added, removed, or columns added, removed, or changed
  in type or nullability.
- Violations keyed by **rule + subject**, never by line, so a moved import is
  not a new violation.

**Notable metric changes** are listed only for Modules in both models that have
metrics, when any of these holds (constants at the top of `diff.go`): |ΔI| or
|ΔD| ≥ 0.2; the zone from `metrics.Zone` changed; the worst CRAP crossed 30 in
either direction, or grew by at least 50% and at least 5 points; the worst CC
rose by 5 or more. The rest is noise in a PR comment.

**Changed subgraph**: Modules added, removed or changed in kind; owners of
changed operations and tables; both ends of every changed dependency and of
every dependency behind a new violation; Modules with a Module-level new
violation. Only Modules that are in the graph without an edge to explain them
(owners, kind changes) get context: one dependent and up to two dependencies,
at most 8 neighbours in all. Hard cap of 30 Modules (the rest counted in
`omitted`). Removed Modules and edges are drawn from base, everything else from
head. Fewer than two Modules means no image.

**Comment length**: sections are capped at 8 bullets ("... and N more"),
dependency lines are grouped per importing Module, Rationales and fix hints are
cut after a whole sentence (260 and 170 characters).

**Image reference**: the Markdown references the image as
`![changed subgraph](diff.png)`: the PNG's path relative to the `--markdown`
file (its bare file name for stdout). `gh pr comment --attach diff.png` uploads
the file and rewrites that reference to the attachment URL, so no placeholder
is needed; `--image-url` sets any other URL. `--attach` cannot upload
with `GITHUB_TOKEN`, so CI posts the comment without an image and the image is
attached from a machine where `gh` runs as a person (see `docs/ci-example.md` and [ADR-0011](0011-pr-comment-image-attached-locally.md)).
Anyone who wants CI to show the image can host the PNG elsewhere (for example
an assets branch) and pass `--image-url`. The image line is
emitted only when `--image` is given and an image is drawn. An unchanged diff
is `<!-- archmodel-diff -->` plus `No architecture changes.` and writes no
image.

**File naming**: `--image P` writes `P.svg` and `P.png`; a `.svg` or `.png`
extension on P is dropped first, so either extension gives the same pair.

**Explorer diff mode**: the head model is overlaid with base-only Modules,
dependencies, operations, tables (and the surface of removed Modules) so removed
things can be shown ghosted. The embedded `archmodel-diff` block carries ids
(`added`, `removed`, `changed`) and the diff itself for the summary card.

## Consequences

The palette gained a `changed` color (amber), used by the Explorer and the
image. Thresholds are opinionated and may need tuning against real PRs.
