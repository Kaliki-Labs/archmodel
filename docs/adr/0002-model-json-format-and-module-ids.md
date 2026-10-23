---
status: accepted
---

# Model JSON v1: deterministic, flat, ID-keyed

The brief asks for a stable, versioned model format (`"version": 1`) that agents,
CI and the Explorer all read. It leaves the shape open.

## Decision

- **Module IDs** are the package path relative to the Go module root, with the
  configured root's strip prefix removed: `backend/internal/auth` → `auth`,
  `backend/cmd/server` → `cmd/server`. This matches how the brief writes the
  expected import graph, and how people talk about the modules.
- The document is **flat and ID-keyed**: top-level arrays of `modules`,
  `dependencies`, `operations`, `schemas`, `tables`, `queries`, `functions`,
  each element referring to others by ID. Nothing is nested more than one
  level deep, so `jq` queries and Phase 2 additions (Dart client calls,
  Flutter features) slot in as new arrays or new `kind`s without reshaping
  existing ones.
- Output is **deterministic**: every array sorted by ID, no timestamps, no
  absolute paths (all paths are relative to the project root, `/`-separated).
  Two builds of the same tree are byte-identical, which golden tests and
  `diff` rely on.
- Additive changes (new optional fields) keep `version: 1`. Removing or
  renaming a field, or changing its meaning, bumps the version.

## Consequences

Consumers must join by ID themselves (the query commands do this for agents).
Absolute paths and build times are not in the model; `serve` tracks freshness
on its own.
