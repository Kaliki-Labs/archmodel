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

Status: **design settled, not yet implemented.** See [`docs/BRIEF.md`](docs/BRIEF.md).
