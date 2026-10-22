# archmodel: implementation brief

Settled in a design session on [kaliki-template#32](https://github.com/Kaliki-Labs/kaliki-template/issues/32).
Terms are defined in [`CONTEXT.md`](../CONTEXT.md). Decisions below are settled;
the parts marked **open** are yours to decide, but record what you chose.

## Why this exists

Code in kaliki-template projects is mostly written by agents. The maintainer
needs two things:

1. **To understand and review the architecture.** The maintainer is still
   learning how to structure modules, so the tool must *teach* (rationale
   everywhere) as well as show.
2. **To constrain agents** so they write code that fits that architecture, and
   to give them a cheaper way to learn the structure than grepping.

Both are served by one **Architecture model**. The Explorer renders it and the
Guardrails check and query it.

## Shape

One Go module, `github.com/Kaliki-Labs/archmodel`, with one binary
`archmodel`. Consumers pin it with a `tool` directive in their `go.mod` and run
`go tool archmodel <cmd>`. Requires Go ≥ 1.26.

Subcommands (names may change, but the capabilities may not):

| Command | Does |
|---|---|
| `build` | Extract the model, write JSON (default `.archmodel/model.json`, gitignored by the consumer) |
| `check` | Evaluate rules; human output by default, `--json` for agents; non-zero exit on violation |
| `query <kind> …` | JSON answers (see Queries) |
| `explore` | Write the self-contained Explorer HTML |
| `serve --addr <host:port>` | Serve the Explorer, rebuild on file change, push reload to open pages |
| `diff --base <model.json> --head <model.json>` | Architecture diff as Markdown (for a PR comment) and/or JSON; `explore --diff` renders the HTML in diff mode |

**Everything project-specific comes from the consumer's `architecture.yaml`.**
The tool hardcodes no kaliki-template paths or names. The template's file is the
reference config, and the tool should ship sensible defaults matching it.

## The model (v1 scope: Go backend + OpenAPI + SQL)

Extract:

- **Modules**: Go packages under configured roots (template: `backend/internal/*`, `backend/cmd/*`). Subpackages such as a domain's generated `gen` belong to their parent module but stay visible at deeper zoom levels.
- **Kind** per module, classified *by convention* from config, with an explicit override list:
  - Domain: a matching spec file exists (template: `api/services/<name>.yaml`). So `health` is a Domain and `payments`, which has no spec, is Infrastructure. That is intended.
  - Composition root: configured list (template: `server`, `cmd/*`, `migrate`).
  - Foundation: configured list (template: `config`, `database`, the sqlc output).
  - Test-only: imported only from `_test.go` files (detect it; don't configure it). Template: `testsupport`.
  - Everything else: Infrastructure.
- **Purpose**: the package doc comment (`// Package x …`). Every package in the template already has one.
- **Dependencies**: imports between modules, marked prod vs test-only. Use `golang.org/x/tools/go/packages`.
- **Public surface**: exported types, functions and interfaces (needed for Abstractness).
- **Operations** from the OpenAPI specs: operationId, method, path, security, `x-rate-limit`, request/response schemas, every documented status code. Each operation is owned by the Domain whose spec declares it, and linked to the Go method implementing it (the domain's `Service` implements the generated `gen.ServerInterface` / strict interface; match on method name = operationId in Go casing).
- **Schemas**: the `$ref` graph, so you can drill from an operation to its request/response schemas to nested schemas.
- **Tables** from goose migrations (template: `backend/sql/schema/*.sql`), with columns. Attribute tables and queries to modules through sqlc query files (template: `backend/sql/queries/<module>.sql`, e.g. `outbox.sql` → `events`). Link each query to the tables it touches.
- **Package metrics** per module: Ca, Ce, Instability `I = Ce/(Ca+Ce)`, Abstractness `A`, Distance `D = |A+I−1|`. Prod graph only.
- **Function metrics**: cyclomatic complexity always. Coverage and **CRAP** (`CC² · (1 − cov)³ + CC`) when a Go coverprofile is supplied (`--coverprofile`). Roll up the worst-function CRAP to the Module card.

Design the model JSON as a stable, versioned format (`"version": 1`). Agents and
CI read it, not only the Explorer.

## Guardrails

### Rules (v1)

Each rule carries an id, a severity and a **Rationale** string, which lives in
config with defaults in the tool. Failure output names the offending edge or
item, gives the Rationale, and gives a concrete fix hint.

1. **Inward only**: dependencies go Composition root → Domain → Infrastructure → Foundation, never outward.
2. **No Domain → Domain**: a Domain never imports another Domain. Fix hint: declare a small interface in the consuming Domain and have the Composition root wire the other Domain into it.
3. **Nothing imports the Composition root.**
4. **Test-only modules stay out of prod code.**
5. **Every module has a package doc comment** (it feeds the Module card).

The template's current code passes all five, and a test should assert that.
Metrics are **displayed, not enforced** in v1. Leave room in config for
threshold rules later.

### Queries

A JSON-output CLI, designed for agents. At minimum:

- `who-depends <module>` / `depends-on <module>`
- `module <name>`: the Module card as JSON
- `operation <operationId>`: method, path, owner, implementing Go func, schemas, documented responses, rate limit, security
- `table <name>`: owner(s), the queries and modules that touch it
- `rules`: every rule with its Rationale (so an agent can read the policy before coding)

No MCP server in v1.

## Explorer

- **One self-contained HTML file**: vanilla JS, a vendored graph library, and the model JSON embedded inline. No npm build step and no framework. **Open**: the graph library (Cytoscape.js, plus ELK or dagre for layout). Keep the file around 2 MB or less, so a lighter layout engine is preferred if it's good enough.
- **Zoom levels**: system → Modules (grouped or colored by Level) → a Module's internals (packages, types, funcs; operations → schemas; tables) → a function (CC/coverage/CRAP, link to source).
- **Module cards** at every level, including Infrastructure modules (they're as drillable as Domains).
- **Teaching**: rule-violating edges in red with their Rationale on hover or tap. Metrics shown with a plain-language reading, e.g. "I=0.9, A=0: unstable and concrete, which is fine for a leaf" and "near (0,0): zone of pain". This matters more than visual polish.
- **Card-first on small screens**: on a phone, navigation is cards plus breadcrumbs, with the graph as a secondary tab. It must be usable from a phone.
- **Diff mode**: given base and head models, highlight added or removed modules, edges, operations and tables, and new violations.
- **Source links**: a configurable URL template (e.g. a GitHub blob URL); file:line otherwise.

## serve

`serve --addr` serves the Explorer, watches the source tree, rebuilds the model
on change and pushes a reload (SSE is fine). There is no auth: the maintainer
runs it on a Mac mini and reaches it over a tailnet from other machines and a
phone. Default to localhost, and only bind elsewhere when `--addr` says so.

## PR artifacts

Two artifacts, both from the `diff` command:

1. **Minimal (primary): a Markdown sticky PR comment.** A short summary of
   modules, dependencies, operations and tables added or removed; new
   violations with their Rationale; notable metric changes; and a **Mermaid
   diagram of only the changed subgraph** (GitHub renders Mermaid, including on
   mobile). Keep it short. When nothing changed, say so in one line.
2. **The full Explorer HTML in diff mode**, uploaded as a CI artifact.

Non-blocking: no CI gating in v1. The template wires up the GitHub workflow;
this repo provides the commands, plus an example workflow in the docs.

## Out of scope for v1

- **Phase 2**: Dart/Flutter, linking the generated Dart client calls to operations by operationId and attaching Flutter features to their Domain. Also crap4dart. Keep the model shaped so this slots in, but don't build it.
- MCP server, Claude Code hooks, what-if/proposal mode, mutation testing, metric-threshold gates.

## Validation

Clone kaliki-template and run against `examples/rendered/{minimal,jwt-full-otp,kitchen-sink}`.
For kitchen-sink, the expected prod import graph between `internal/*` modules is:

```
auth     -> config database mail ratelimit
items    -> database
cache, events, mail, payments, push, storage -> config
server   -> auth cache config events health items mail migrate observability payments push ratelimit storage
cmd/server  -> server
cmd/workers -> config database events observability
testsupport -> migrate   (test-only; auth and items import it from tests)
```

Domains are `auth`, `health` and `items`, and all five rules pass. `internal/database` (sqlc output) and the `gen` packages are generated and
gitignored in the rendered examples, so run the project's codegen first (see
its makefile/README) or have the extractor tolerate missing generated packages.
Golden-test the model JSON and the diff Markdown against these fixtures.

## Template-side wiring (not this repo; done later in kaliki-template)

- A `tool` directive in `backend/go.mod`, plus `architecture.yaml` at the project root.
- `make arch`, `make arch-serve`, `make arch-check` and `make arch-diff`; `arch-check` runs from `fitness_checks/run_all.sh`.
- Generated CLAUDE.md: run `make arch-check` before finishing; use `go tool archmodel query …` to learn the structure.
- A CI workflow that builds base and head models, posts the sticky comment and uploads the HTML.
- Always generated, with no Copier flag.
- Template CI runs the tool against the rendered examples, so tool or template breakage shows up quickly.
