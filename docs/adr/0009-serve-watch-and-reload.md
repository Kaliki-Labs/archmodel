---
status: accepted
---

# serve: polling watcher, SSE reload, last good model

The brief asks `serve` to rebuild on file change and push a reload to open
pages, with no auth and localhost by default. It leaves the mechanism open.

## Decision

- **Polling, not fsnotify.** Every second the server walks the Go module
  directory, the specs, schema and queries directories, and the config file
  (plus the coverprofile, if given) and hashes path, mtime and size of `.go`,
  `.yaml`/`.yml`, `.sql`, `.mod` and `.sum` files. It skips `.git`,
  `node_modules`, `.archmodel`, `build`, `dist`, `target`, `.dart_tool` and
  `vendor`. A walk of a project this size takes milliseconds, works the same on
  every OS and over network mounts, and adds no dependency.
- **Debounce.** After a change is seen the server waits 300ms, takes a new
  fingerprint as the baseline, then rebuilds, so a burst of saves is one
  rebuild and a save during the build triggers another.
- **SSE, whole-page reload.** `/events` streams `reload` after a successful
  rebuild. The page calls `location.reload()`, and the URL hash route
  survives. The model is embedded in the HTML, so there is no state to patch.
- **Last good model.** A failed rebuild leaves the served model and page
  untouched and streams `builderror` with the message; the page shows a banner.
  The next successful rebuild reloads the page and clears it. A failed first
  build is a startup error, since nothing exists to serve.
- **Config is reloaded on every build**, so editing `architecture.yaml` takes
  effect.
- **Network.** Binds `127.0.0.1:7878` by default; any other address needs
  `--addr`, and the server warns when it is not loopback. No auth: the intended
  deployment is a Mac mini reached over a tailnet.
- **Shutdown.** SIGINT/SIGTERM closes the SSE streams (they would otherwise
  block shutdown) and then shuts the HTTP server down gracefully.

## Consequences

Changes are noticed within about 1.3s. A change to a file type outside the
list (for example a new extension for specs) is not noticed until the next
relevant edit.
