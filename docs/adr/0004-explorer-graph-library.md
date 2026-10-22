---
status: accepted
---

# Explorer graph library: Cytoscape.js with dagre layout

The brief leaves the graph library open (Cytoscape.js with ELK or dagre), with
a budget of about 2 MB for the self-contained HTML file.

## Decision

Vendor **Cytoscape.js** and **cytoscape-dagre** (with **dagre**), minified,
into the binary with `go:embed`, and inline them into the Explorer HTML.

- dagre does layered (Sugiyama) layouts, which suit a dependency graph
  with Levels: Composition root on top, Foundation at the bottom.
- ELK lays out somewhat better but adds roughly 1.5 MB, most of the budget,
  for little gain at the size of these graphs (tens of Modules).
- Library versions and licenses are recorded in `internal/explorer/assets/`
  next to the vendored files.

## Consequences

The Explorer has no network dependency and no npm step. A typical HTML
file is about 1 MB plus the model. Upgrading a library means
replacing the vendored file by hand.
