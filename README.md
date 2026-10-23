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
work, as do the rules, queries, the Explorer and `serve`; `diff` is still to come. See
[`docs/BRIEF.md`](docs/BRIEF.md).

## Running it

A consumer pins the tool with a `tool` directive in `backend/go.mod` and runs
it from `backend/`, the directory holding `go.mod`:

```sh
cd backend && go tool archmodel check
```

With no `--root`, the project root is the nearest directory above the current
one that holds `architecture.yaml` (else `.`). `--config` defaults to
`<root>/architecture.yaml`; an explicit `--config` or `--out` is relative to
the current directory, while default outputs such as `.archmodel/model.json`
land under the root. An explicit `--root` disables the search. The examples
below use `go run ./cmd/archmodel` to work on this repository itself.

## build

```sh
go run ./cmd/archmodel build --root path/to/project --config architecture.yaml
# or, in a consumer project: cd backend && go tool archmodel build
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

## explore

```sh
go run ./cmd/archmodel explore --root path/to/project --config architecture.yaml
```

Writes the **Explorer** as one self-contained HTML file (`--out`, default
`.archmodel/explorer.html`; about 0.8 MB, no network needed). It builds a fresh
model like `check` does, or reads one with `--model model.json`.

- **System** view: Modules coloured by Level (Composition root, Domain,
  Infrastructure module, Foundation), layered top to bottom. Test-only modules
  sit apart and dimmed; both they and test-only edges (dashed) can be toggled.
- Click or tap a Module to drill in: its **Module card** (purpose, Level,
  metrics with a plain-language reading and the metric Rationales, depends-on,
  depended-on-by, what it owns, public surface, worst functions, violations)
  and a graph of its subpackages (including generated ones that are "gen, not
  built"), operations, tables and queries. Operations drill down to their
  schemas along `$ref`; tables to their columns, foreign keys and queries; and
  functions to CC, coverage and CRAP. Infrastructure modules are exactly as
  drillable as Domains.
- **Teaching**: an edge that breaks a rule is red. Hover it, or tap it (a sheet
  opens, so this works on a phone), to see the Rationale and a fix hint. All
  teaching text comes from the model.
- Every view has a URL (`#/module/auth`, `#/operation/login`, `#/table/users`,
  `#/function/auth.Service.Login`), so links are shareable and the back button
  works. The search box finds Modules, operations, tables, schemas and
  functions (press `/`).
- **On a phone** (below 700px) navigation is cards and breadcrumbs, and the
  graph is the second tab.
- **Source links**: set `source.url_template` in `architecture.yaml`, for
  example `https://github.com/ORG/REPO/blob/{ref}/{path}#L{line}`, and pass
  `--ref main` (or a commit). Without a template, or if it uses `{ref}` and no
  `--ref` is given, the Explorer shows copyable `path:line` text.

## diff

```sh
archmodel build --root base-checkout --out base.json
archmodel build --root . --out head.json
archmodel diff --base base.json --head head.json \
  --markdown comment.md --json diff.json --image diff
archmodel explore --diff --base base.json --head head.json --out diff.html
```

The **Architecture diff** between two models: Modules, dependencies
(production and test-only), operations (route, security, rate limit,
responses), tables and columns added, removed or changed; **new violations**
(matched on rule and subject, so moved lines do not count) with their
Rationale and fix hint, and resolved ones; and notable metric changes (see
[ADR-0010](docs/adr/0010-architecture-diff.md) for the thresholds).

- `--markdown <path|->` (default `-`, stdout): a short sticky PR comment that
  starts with `<!-- archmodel-diff -->`. Sections appear only when non-empty,
  and long lists end in "... and N more". With no changes the whole comment is
  `No architecture changes.` and no image is written.
- `--image <path>`: draws only the changed subgraph and writes `<path>.svg` and
  `<path>.png` (a `.svg` or `.png` extension is dropped first, so `--image
  out/diff.png` writes both). Pure Go, no external tools; the PNG is at most
  1200 px wide. The Markdown references the PNG by `--image-url` (default
  placeholder `ARCHMODEL_IMAGE_URL`) for the CI workflow to replace after
  uploading, and folds a Mermaid version of the same subgraph in a `<details>`
  block (`--mermaid=false` to omit it).
- `--json <path|->`: the diff as deterministic JSON (`"version": 1`), including
  the subgraph.
- `explore --diff --base base.json [--head head.json]` renders the Explorer in
  diff mode: a diff card and the new violations at the top of the System view,
  added things outlined green, changed ones amber, and removed ones ghosted
  (dashed red, drawn from the base model). Without `--head` it builds or reads
  the head like `explore` does.

A GitHub workflow that posts the comment is in
[docs/ci-example.md](docs/ci-example.md). It never fails the build.

## serve

```sh
go run ./cmd/archmodel serve --root path/to/project --addr 127.0.0.1:7878
```

Serves the Explorer at `/`, the current model at `/model.json` and a
Server-Sent Events stream at `/events`. It polls the Go, spec, SQL and config
files about once a second, rebuilds when they change, and open pages reload
themselves, keeping their route. If a rebuild fails it keeps serving the last
good model and shows the error in a banner. Ctrl-C stops it. See
[ADR 0009](docs/adr/0009-serve-watch-and-reload.md).

There is **no authentication**, so it binds to localhost unless you say
otherwise. To use it from a phone or another machine over a tailnet, bind to
the machine's tailnet address (or `0.0.0.0` on a network you trust):

```sh
archmodel serve --addr 100.101.102.103:7878   # then open http://<mac-mini>:7878/ on the phone
```

## Config

The template's conventions (Go module in `backend/`, Modules under
`internal/*` and `cmd/*`, specs at `api/services/<name>.yaml`, the Composition
root and Foundation lists, and a Rationale for each rule) are built in, in
[`internal/config/defaults.yaml`](internal/config/defaults.yaml). A project's
`architecture.yaml` is merged over them: maps merge key by key, scalars and
lists replace, unknown keys are an error. A project that follows the template
needs only a few lines; see
[`docs/architecture.example.yaml`](docs/architecture.example.yaml).
