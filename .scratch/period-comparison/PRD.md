# PRD — Period comparison

## Goal

Ship the next differentiator after derived Metrics: **period comparison** — read
a whole Dashboard against a second, earlier window and overlay it on every Panel.
"This week vs last week", "this July vs last July", or an arbitrary custom span,
chosen once at the Dashboard level and saved with it.

Context, glossary, and the design decision: see `CONTEXT.md`
(**Baseline**, **Baseline rule**, **Ordinal alignment**) and `docs/adr/0015`.

## What this milestone does

- Adds a **Baseline** to the Dashboard's time axis — a Dashboard-wide, persisted
  second window, defined by a **Baseline rule**:
  - `previous` — shift the current range back by its own length.
  - `same_period_last_year` — shift the current range back one year.
  - `custom` — absolute frozen `from`/`to` dates.
  - `none` (default) — comparison off.
- Extends the **query engine** to resolve the baseline window from the rule
  (calendar-aware) and align it to the current series by **ordinal position**,
  **truncated to the shorter** window. Each baseline bucket keeps its real date.
- Serves both windows through the **API** in one request (`/v1/series?baseline=…`
  returns current + baseline series, equal length, index-aligned; the Dashboard
  payload carries the baseline fields).
- Renders the baseline in the **SPA** as one uniform muted/dashed line across all
  chart types; a single Dashboard-level comparison control; disabled at range
  `all`.

## What this milestone does NOT do

- **Trends** — rolling/smoothed lines over a single window, and the
  `body_mass`/`latest` vs "weekly average" tension. Deferred to its own milestone.
- **A computed delta** — no "+12% vs previous" headline and no per-bucket delta
  series (ill-defined for derived Metrics; belongs with trends).
- **Per-Panel comparison** — comparison is Dashboard-wide; a per-Panel override
  can be added additively later.
- **Per-chart-type overlay** (e.g. grouped bars for bar Panels) — one uniform
  line for now.
- **Comparing more than two windows** at once.

## Design invariants (from ADR 0015)

- The Baseline is a **Dashboard-wide** property of the time axis, persisted in
  three columns mirroring the `range_*` columns; Panels are unchanged.
- Relative baseline rules are **recomputed** from the current range, never stored
  as dates; only `custom` is absolute.
- Alignment is **ordinal, truncated to the shorter** window — never by calendar
  date. Baseline buckets keep their own real dates for tooltips.
- Baseline date math and truncation are **server-side**; the client never
  computes a baseline window.
- Comparison is a **pure overlay** — no computed delta — so it works uniformly
  for imported and derived Metrics.
- Comparison is **disabled when the range is `all`**; for `1y` the two relative
  rules coincide.
- An empty baseline window renders **no line** (gaps, never zero).

## Slices (issues)

1. **Dashboard schema + model**: migration adding `baseline_rule`/`baseline_from`/
   `baseline_to`, DAO fields, validation mirroring the range columns.
2. **Query engine**: baseline-window resolver (rule → concrete window) and the
   ordinal truncate-to-shorter aligner, over the existing bucket query.
3. **API**: `/v1/series` `baseline` param returning both series index-aligned;
   Dashboard payload read/write of the baseline fields.
4. **SPA**: Dashboard-level comparison control, uniform muted-line baseline across
   chart types, disabled at range `all`.
