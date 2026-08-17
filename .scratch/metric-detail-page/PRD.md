# PRD — Metric page

## Goal

Give every Metric its own full page — the way Apple Health opens a dedicated
screen for "Weight" or "Steps": a big current figure, a real trend chart, the
period's highs/lows, and the chronological history — instead of the Ledger's
cramped inline expand-row. One place to actually look *at* a Metric, not just
read its numbers off a table.

## What this milestone does

- A new route, `/data/$metric`, rendering the **Metric page**: header (icon +
  label, back to Data), a range control (7D/30D/3M/1Y/All, same presets as
  everywhere else), the **Panel summary** band (reused as-is — big figure,
  latest reading, no Baseline here), the trend chart (reused `PanelChart`,
  single Series, same chart-type default as a Panel), a **Highs & lows** stat
  row (max/min bucket value over the current range, computed client-side from
  the already-fetched points — no new aggregation), and the existing
  `LedgerDetailTable` (day/week/month history, unchanged) underneath.
- The Ledger's Scoreboard row stops expanding inline: clicking a row now
  navigates to its Metric page. The overview table goes back to being just the
  overview (ADR 0021's first face); the detail face moves to its own page,
  which also has room for the chart the inline row never had.
- Entry point is the Data page and every Panel: a single-Metric Panel's title
  and a multi-Metric Panel's per-Metric legend entry both link to that
  Metric's page — the same identity, wherever its name is shown. No change to
  Dashboards or any server endpoint — everything here is served by the
  existing `GET /v1/series`.

## What this milestone does NOT do

- **No new server endpoint or field.** `/v1/series` already carries points,
  `summary`, `mean`, and `days`; the highs/lows stat is a plain `Math.min`/`max`
  over points already on the client, the same kind of client-side read the
  Ledger's delta and `mergeSeries` already do.
- **No comparison/Baseline on the Metric page.** Period comparison is a
  Dashboard-wide concept (ADR 0015); the Metric page is not a Dashboard and
  does not carry a Time axis of its own to compare against. Same exclusion
  precedent as a multi-Metric Panel dropping the Baseline (ADR 0020).
- **No editing from this page.** "Enter a value" stays a Data-page-level
  action (`ManualEntryDialog`), not duplicated here.

## Issues

1. `01-web-metric-page` — web: the route, the Metric page, and the Ledger row
   linking to it instead of expanding inline.
2. `02-panel-links` — web: link a Panel's title (single-Metric) and each
   legend entry (multi-Metric) to their Metric page.
