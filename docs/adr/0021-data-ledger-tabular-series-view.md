# The Data Ledger — a tabular view over the Series layer

## Context

Verve only ever shows data as graphs. A curve is good for shape but never lets you
read an exact value, sort by it, or copy it out. Users want "un endroit avec les données
en tableau" — the numbers behind the graphs — to inspect a Metric day by day, compare a
week to the last, and paste figures into a spreadsheet.

The obvious reading, "raw data", is a trap. The `measurements` table stores one row per
source record at native granularity — hundreds of thousands of rows per Metric (~238k for
`heart_rate` on the reference export) — and ADR 0012 deliberately serves only aggregated
buckets, never raw rows. Grilling confirmed the real need is **the aggregated Series shown
as tables**, not the underlying samples: "masse au jour j, moyenne à la semaine/mois,
comparaison à la semaine dernière… le but est d'avoir les données des graphes mais sous
forme de tableau."

## Decision

Introduce the **Ledger**: a tabular read-view over the existing **Series** layer,
surfaced as a dedicated **"Data"** page. It has two faces on one page:

- **Overview** — one row per Metric that has data: its latest value, folded window
  figures (~7-day, ~30-day), and a delta versus the previous week.
- **Detail** — one Metric's Points as chronological rows at a chosen bucket
  (`day` / `week` / `month`), with a per-row delta versus the previous row.

Each table has a **Copy** button that copies the whole visible table as **TSV with dot
decimals** — the format that pastes cleanly into Sheets, Excel, and Notion without setup.

The detail table reuses `GET /v1/series` unchanged (points + summary). The overview adds
one small endpoint, **`GET /v1/ledger`**, that lists the Account's Metrics-with-data (a
new `DistinctMetrics` query over `measurements`) and folds each window figure by reusing
the query engine's server-side summary and comparison. No raw-row endpoint is added.

## Considered Options

- **Reuse `/v1/series` + one thin `/v1/ledger` overview (chosen).** All folding stays
  server-side (ADR 0012, ADR 0019): the overview's window figures are true engine
  summaries, and the detail table's per-row delta is a trivial `point[i] − point[i-1]`
  display calc, not a re-aggregation. Minimal new surface.
- **A raw-samples endpoint over `measurements`.** What "raw data" literally says, but it
  contradicts ADR 0012, would expose every Source (the graph picks one winner, ADR 0003),
  and needs real server-side pagination/virtualization for 10⁵–10⁶ rows per Metric. Not
  what the user wants (they think in daily/weekly figures). Rejected.
- **Client-side folding of bucket values for the overview.** Free, no endpoint, but a
  mean of per-bucket means is biased for `average` Metrics and violates ADR 0012/0019.
  Rejected.

## Consequences

- The overview's window figure is the metric's own-rule summary (count-weighted,
  ADR 0019). For a `sum` Metric it is divided server-side by the window's day count so it
  reads as a **daily average** ("~7-day"), matching how the user thinks of steps/calories.
- `duration_by_state` (sleep) is stored in `states`, not `measurements`, so it does not
  appear in the Ledger — consistent with it not yet being on the series path (ADR 0018).
- The Ledger is read-only and additive: no schema change, `/v1/series` unchanged, and a
  per-Panel "view as table" toggle can later reuse the same detail component.
- Copy is TSV-only for now; CSV/Markdown/JSON variants are a later, additive menu.
