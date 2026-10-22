---
status: accepted
---

# Tolerate missing generated packages; link operations by method name

The rendered kaliki-template examples gitignore generated code
(`internal/database` from sqlc, `gen/api/<domain>` from oapi-codegen) and have
no `go.sum`. CI and agents will often run `archmodel` before codegen. The brief
allows either running codegen first or tolerating the gaps.

## Decision

The extractor **tolerates missing packages**, and never requires codegen,
network access or a complete `go.sum`.

- A missing package imported from a configured module root still becomes a
  Module, marked `"generated": true, "present": false`. Its kind comes from
  config as normal (`database` is still Foundation). The doc-comment rule
  skips Modules that are not present, since there is no source to comment.
- Generated subpackages (`gen/api/<name>`) are mapped to their parent Module
  through a configured pattern and appear as subpackages of it, whether or not
  they exist on disk.
- Operations are linked to Go methods **syntactically**: a method on the
  Domain's configured implementer type (default `Service`) whose name is the
  operationId in Go casing. This does not need the generated
  `ServerInterface` to type-check.
- Type-checking errors from `go/packages` are recorded as build warnings
  (stderr, and a `warnings` array in the model), never fatal.

## Consequences

Public surface and metrics for a missing generated package are empty, so its
Abstractness is 0. When codegen has run, the same extractor picks the package
up with no config change. The model records `present` so the Explorer can
show "generated, not built" instead of an empty card.
