# 06 — Embedded SPA: Dashboards / Panels / Time range

Status: ready-for-agent
Blocked by: 04, 05

## Goal

The user-facing payoff: a React SPA, embedded in the binary, where a logged-in
Account builds Dashboards of single-metric Panels and reads their graphs.

## Scope

- **Front-end app**: plain Vite + React SPA with **TanStack Router** (client-side
  routing), **TanStack Query** (fetching the Go API), **TanStack Table** (tabular
  views), **shadcn/ui** components, **Recharts** for charts. Hotkeys via
  `react-hotkeys-hook` or a custom hook. **Not** TanStack Start (ADR 0013).
  Dev mode runs the Vite dev server proxying to the Go API; production build is
  embedded via `go:embed` and served by the Go server (ADR 0005). The Go server
  serves `index.html` as fallback on all non-`/v1/*` routes.
- **Auth UI**: login screen, session handling, logout.
- **Dashboards** (Account-scoped, persisted): create/rename/delete; switch
  between them. `dashboards` + `panels` tables + CRUD endpoints.
- **Panel**: config = metric × chart type × aggregation (from Catalog default) ×
  bucket. Renders against `GET /v1/series`. Chart types: **bar**, **line**,
  **area**, plus a **min/max band** variant for `average` Metrics and **stacked
  bar** for sleep (`duration_by_state`). The default chart type is derived from
  the Metric's aggregation rule (sum→bar, average→line+band, latest→line,
  duration_by_state→stacked bar); the user can switch among compatible types.
  No scatter/pie/radar in v1.
- **Time range**: global per Dashboard, applied to all Panels. Preset buttons
  (`7d` / `30d` / `3m` / `1y` / `All`) + a custom start–end **range picker**
  (shadcn calendar range, day granularity, no time-of-day in v1). **Bucket
  auto-derived** from the span (≤31d→day, ≤1y→week, >1y→month), overridable per
  Panel. No intraday zoom in v1 (→ v1.x, tied to the capped-resolution API).
  Single range only — period *comparison* is v1.1.
- **Layout**: responsive grid; each Panel picks a width preset (span 1–3
  columns) and Panels are **drag-reorderable** via **dnd-kit**. No free-form
  pixel resize in v1 (→ v1.x). Captures the "my dashboard" feel (which cards,
  what order, what size) without a full grid-layout engine.
- Dark mode (shadcn theming).

## Out of scope

Rich workout UI + GPS map (v1.x). Cross-metric / period comparison / annotations
(post-v1). Drag-and-drop grid polish can be minimal in v1.

## Acceptance

- Fresh build produces a single binary serving the SPA; logging in shows the
  user's Dashboards.
- Adding a "Steps — daily — sum — bar" Panel renders real imported data.
- Changing the Dashboard time range updates all Panels.

## Refs

ADR 0005 (embedded SPA), 0012 (aggregated API + shadcn/Recharts), 0013 (TanStack
libs, not Start). Glossary: Dashboard, Panel, Time range.
