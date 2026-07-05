# Client-rendered SPA with TanStack libraries, not TanStack Start

## Context

The dashboard is a Vite + React SPA embedded in the Go binary via `go:embed`
(ADR 0005), with Go owning the JSON API. The TanStack ecosystem is attractive;
the question is whether to adopt **TanStack Start** (the full-stack meta-framework)
or just its standalone libraries.

## Decision

Use a **plain Vite SPA** with standalone **TanStack Router** (client-side,
type-safe routing), **TanStack Query** (server-state against the Go API), and
**TanStack Table** (tabular views), alongside **shadcn/ui** and **Recharts**.
Hotkeys via a small library (`react-hotkeys-hook`) or a custom hook. **Do not**
adopt TanStack Start.

The Go server serves `index.html` as a fallback on all non-`/v1/*` routes so
client-side routing works.

## Why

TanStack Start is a full-stack framework (SSR, server functions, streaming) that
needs a Node/Nitro runtime — directly conflicting with ADR 0005's single Go
binary and no-Node-in-production constraint. Its server-function model solves a
problem we don't have (Go is the backend); adopting it would mean either two
backends or paying for a build/routing model we'd never fully use. The standalone
libraries give routing, data-fetching, and tables with zero server runtime, and
compose cleanly with shadcn (components) and Recharts (charts) as complementary
layers.

## Consequences

- The Go server needs an SPA fallback route for client-side routing.
- No SSR/SEO — acceptable for a self-hosted, authenticated dashboard.
