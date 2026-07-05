# Aggregated-bucket API contract; shadcn/ui + Recharts front-end

## Context

A single Metric can hold hundreds of thousands of points (238k `heart_rate`
readings). Shipping raw series to the browser would kill the SPA. Separately, the
UI needs far more than charts: a dashboard/panel builder, config forms, dialogs,
account management, date/range pickers.

## Decision

The JSON API **never returns raw series**. A Panel requests
`metric + time range + bucket`; the server applies the Metric's aggregation rule
in SQL and returns at most a few hundred points. Zoom re-requests a finer bucket,
with a **capped maximum resolution** (never below, e.g., per-minute for heart
rate) so the raw series is never shipped. Waveforms (deferred ECG) are the sole
exception — read whole from their file, outside this path.

Because charts therefore only ever see bucketed data, chart-library performance
is a non-issue. The front-end uses **shadcn/ui** (Radix + Tailwind, copied into
the repo → no lock-in) for the whole UI system and **Recharts** (which shadcn's
charts wrap) for time-series. **uPlot** is reserved for the future ECG waveform
viewer, where high-resolution canvas rendering actually earns its keep.

## Why

Server-side bucketing bounds payload size regardless of history and makes the
per-bucket aggregation SQL the core of the query engine. Having deliberately made
charts perf-insensitive, the uPlot performance argument evaporates; shadcn/ui's
cohesive, self-owned component system is worth more than raw chart speed for a
UI-heavy dashboard app.

## Consequences

- Per-bucket aggregation SQL is the heart of the query engine.
- The API must expose and enforce a max-resolution cap per Metric.
