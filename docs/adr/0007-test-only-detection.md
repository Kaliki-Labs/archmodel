---
status: accepted
---

# Test-only detection: no prod importer, plus test evidence

The brief says a Module is Test-only when only `_test.go` files import it. That
misses support packages that no test imports yet: `testsupport` in the
`minimal` example has no importer at all.

## Decision

A Module is **Test-only** when no production code imports it **and** at least
one of these holds:

- a `_test.go` file imports it, or
- its own non-test files import `"testing"`.

Composition roots (and anything matched by the configured lists or
`kinds.overrides`) are classified first and are never Test-only. A Module
nothing imports and that doesn't import `"testing"` stays Infrastructure, so
unused production code isn't hidden from the production graph.

## Consequences

No configuration is needed for `testsupport` in any of the template examples.
A production package that imports `"testing"` and has no prod importer would be
misclassified; override it with `kinds.overrides`.
