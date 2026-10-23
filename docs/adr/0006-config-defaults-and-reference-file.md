---
status: accepted
---

# Config: embedded defaults plus the consumer's architecture.yaml

The brief says everything project-specific comes from the consumer's
`architecture.yaml`, that the tool hardcodes no template paths, and that the
tool ships defaults matching the template.

## Decision

- The tool embeds a `defaults.yaml` that encodes the template's **conventions**
  (Go module in `backend/`, roots `internal/*` and `cmd/*`, specs at
  `api/services/<name>.yaml`, migrations, sqlc query files, Composition root
  and Foundation lists, rule Rationales). The consumer's `architecture.yaml`
  is merged over it: scalars and maps override key by key, and lists replace
  wholesale.
- Go code never names a template path or module. Every such string lives in
  `defaults.yaml`.
- Mappings that aren't conventions, such as `outbox.sql` → `events`, live only
  in the consumer's file. `docs/architecture.example.yaml` is the reference
  config for kaliki-template, and the fixture tests use it.
- `version: 1` at the top of the config, and unknown keys are an error, so
  typos fail loudly.
- A `thresholds:` section is reserved for future metric rules and accepted but
  ignored in v1.

## Consequences

A project that follows the template conventions needs only the few lines
of overrides. A project that doesn't can override everything.

Commands run from the directory holding `go.mod` (`go tool` requires it), so
with no `--root` they find the project root by walking up to the nearest
`architecture.yaml` (else `.`), and `--config` and default outputs resolve
against that root.
