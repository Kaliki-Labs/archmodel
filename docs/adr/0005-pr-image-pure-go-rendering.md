---
status: accepted
---

# PR image: pure-Go layered layout to SVG and PNG

`diff` has to render an image of the changed subgraph for the PR comment. The
brief leaves the rendering method open and prefers CI to stay
dependency-free.

## Decision

Render in **pure Go**. No headless browser, Node or Graphviz.

- A small layered layout: rank by Level (Composition root → Foundation), then
  order within a rank by barycenter to reduce crossings. The changed subgraph is
  small (changed Modules plus their direct neighbours), so this is good enough.
- One layout feeds two writers: an **SVG** writer (text), and a **PNG**
  writer that draws the same shapes with `golang.org/x/image/vector` and the
  Go fonts (`golang.org/x/image/font/gofont`).
- Styling comes from one shared palette (Level colors, added/removed
  highlighting, red violation edges) that the Explorer's CSS uses too, so the
  image and the Explorer look the same.

## Consequences

The image won't match the Explorer's dagre layout pixel for pixel, but it
uses the same colors and the same meaning. CI needs only `go`.
