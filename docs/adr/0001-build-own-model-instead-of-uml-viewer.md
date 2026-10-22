---
status: accepted
---

# Build our own Architecture model instead of adopting uml-viewer

[kaliki-template#32](https://github.com/Kaliki-Labs/kaliki-template/issues/32)
started as an experiment with Robert C. Martin's
[uml-viewer](https://github.com/unclebob/uml-viewer). We take its ideas
(dependency-rule levels with violating edges in red, CRAP-colored boxes,
click-through to source, an agent-facing channel) but not its runtime.

uml-viewer has no Go or Dart scanner. It needs Clojure and Java 21, and it's a
desktop app, while we need to view it from other machines, phones and PRs. Its
model is classes, while ours has to be Domains as vertical slices: spec,
handlers, tables. Writing a Go scanner for it would still leave the
desktop-only viewer and a class-level model that doesn't fit.

## Considered Options

- **Adopt uml-viewer and write Go/Dart scanners**: rejected for the reasons above.
- **The .NET port's web UI**: solves remote viewing, but carries the same class-level model and adds a .NET runtime.
- **Static diagrams only (Mermaid/SVG)**: kept for the minimal PR comment, but can't drill down or answer agent queries.

## Consequences

We maintain an extractor, a web Explorer and a check engine ourselves. In
return, one model serves humans and agents, and conventions live in config.
