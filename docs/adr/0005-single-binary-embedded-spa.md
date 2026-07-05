# Single binary: Go server with embedded React SPA

## Context

Verve targets homelab self-hosting where operational simplicity matters. The
dashboard is a graph-heavy UI the user wants to build with Vite + React.

## Decision

Ship a **single executable**. The Go server exposes a JSON API and serves a
Vite-built React SPA embedded via `go:embed` (`embed.FS`). SQLite lives as a
`.db` file beside the binary. Deployment is one binary; backup is copying the
`.db` file (plus the artifacts directory).

## Why

One artifact to deploy and version, no separate web server or Node runtime in
production, clean cross-compilation. The API/SPA split keeps a clear boundary
that the community can build alternative clients against later.

## Consequences

- The build has two stages: `vite build` produces static assets, then the Go
  build embeds them. Dev mode runs Vite's dev server proxying to the Go API.
- The Go/React boundary is a versioned JSON API — it must be treated as a real
  contract, not an internal detail.
