# archmodel

Extracts an **Architecture model** from a Go + OpenAPI + SQL codebase and puts
it to two uses:

- **Explorer**: a human-facing, drill-down view of the modules, their
  dependencies, the API operations and schemas they own, and their tables,
  with coupling and CRAP metrics. It runs as a live server you can open from
  another machine or a phone, and in diff mode for reviewing PRs.
- **Guardrails**: agent-facing *checks* (dependency rules that fail with a fix
  hint and a rationale) and *queries* (a JSON CLI an agent asks instead of
  grepping: "who depends on `auth`?").

Consumed as a pinned Go tool (`go tool archmodel …`), with all project-specific
conventions in the consuming project's `architecture.yaml`. The first consumer is
[kaliki-template](https://github.com/Kaliki-Labs/kaliki-template).

Status: **partly implemented.** The Architecture model and `archmodel build`
work; rules, queries, the Explorer, `serve` and `diff` are still to come. See
[`docs/BRIEF.md`](docs/BRIEF.md).

## build

```sh
go run ./cmd/archmodel build --root path/to/project --config architecture.yaml
```

Extracts the Architecture model and writes it as JSON (`--out`, default
`.archmodel/model.json`, `-` for stdout). Add `--coverprofile cover.out` to get
per-function coverage and CRAP. The output is deterministic: sorted, relative
paths, no timestamps, so two builds of the same tree are byte-identical. The
format is described in [ADR 0002](docs/adr/0002-model-json-format-and-module-ids.md).

The extractor never needs codegen, network access or a complete `go.sum`:
generated packages that are missing on disk still show up as Modules (with
`present: false`), and anything it could not read is reported in the model's
`warnings` array and on stderr.

## Config

The template's conventions (Go module in `backend/`, Modules under
`internal/*` and `cmd/*`, specs at `api/services/<name>.yaml`, the Composition
root and Foundation lists, and a Rationale for each rule) are built in, in
[`internal/config/defaults.yaml`](internal/config/defaults.yaml). A project's
`architecture.yaml` is merged over them: maps merge key by key, scalars and
lists replace, unknown keys are an error. A project that follows the template
needs only a few lines; see
[`docs/architecture.example.yaml`](docs/architecture.example.yaml).
