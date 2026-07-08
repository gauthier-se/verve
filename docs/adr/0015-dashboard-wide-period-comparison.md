# Dashboard-wide period comparison, server-aligned by ordinal position

## Context

The v1 core and derived Metrics are shipped. The next differentiator is **period
comparison**: reading a Dashboard against a second, earlier time window — this
week vs last week, this July vs last July, or an arbitrary custom span. The
derived-metrics PRD bundled "period comparison / trends" as one lane; they are
distinct features (a second window overlaid vs a smoothed line over one window,
with different data and UI). This milestone ships **comparison only**; trends
and the `latest`-vs-rolling-average question are deferred.

Several shapes had to be pinned: what owns the baseline, how a baseline window is
defined, how two windows of unequal length share one chart, where alignment is
computed, and whether comparison yields a number.

## Decision

Period comparison is a **Dashboard-wide** property of the time axis, the natural
companion to the Time range that already lives on the Dashboard (CONTEXT.md,
ADR 0012). A single control puts the whole board into comparison mode; Panels
stay "Metrics × chart × aggregation × bucket" and render whatever windows the
Dashboard hands them.

The second window is a **Baseline**, defined by a **baseline rule**:

- `previous` — shift the current range back by its own length.
- `same_period_last_year` — shift the current range back one year.
- `custom` — absolute frozen `from`/`to` dates (same shape the range's `custom`
  preset already uses).
- `none` (default) — comparison off.

The baseline **persists on the Dashboard** as three columns — `baseline_rule`,
`baseline_from`, `baseline_to` — mirroring the existing `range_preset`/
`range_from`/`range_to` pattern exactly.

The two windows are aligned by **ordinal position within the period** (bucket 1
vs bucket 1, "day 1 of each window"), **truncated to the shorter** of the two —
never by calendar date, since the dates differ by construction. Each baseline
bucket keeps its own real date for tooltips.

Alignment is **server-side, one request**. `/v1/series` gains a `baseline`
parameter; the server computes the baseline window from the rule (calendar-aware
date math), runs the existing bucket query over it, truncates both series to
equal length, and returns them index-aligned. The client never does baseline
date math.

Comparison is a **pure visual overlay** — no computed delta. The baseline
renders as **one uniform muted/dashed line** across every chart type (bar, line,
area, band, stacked_bar, and the diverging bar for signed derived Metrics).

Comparison is **disabled when the range is `all`** (there is no "before all").
For range `1y` the two rules coincide; that is harmless and unspecialized.

## Why

- **Dashboard-wide, on the time axis.** The baseline *is* a property of the time
  window, and the window belongs to the Dashboard. This keeps the model
  orthogonal (Dashboard owns the time axis, Panels own the metric axis) and the
  UI to a single control. Per-Panel comparison stays available as an additive
  override later, like the per-Panel bucket override already is.
- **Rules, not stored dates, for relative baselines.** "Same period last year"
  over a rolling range must be recomputed, not frozen; only `custom` is
  absolute — and that shape (`*_from`/`*_to`) already exists on the range.
- **Ordinal alignment, truncated to shorter.** Calendar alignment is impossible
  (the point of comparison is different dates). Ordinal overlay is the only
  coherent reading; truncating to the shorter window compares like-for-like
  elapsed time and drops orphan baseline buckets (leap-day, longer custom span)
  that have no counterpart.
- **Server-aligned, one request.** Baseline-rule date math and truncation are
  domain logic that belong next to the query engine — computed once, tested
  once, provably consistent — not reimplemented in TypeScript across two
  round-trips that can drift.
- **Pure overlay, no delta.** A window-level delta requires collapsing a window
  to one number, which is well-defined for imported Metrics but **ill-defined
  for derived ones** (no aggregation rule of their own, ADR 0014). A pure
  overlay works uniformly for both; a headline delta is a trends-lane concern
  and is deferred with it.
- **One uniform baseline line.** A single rendering rule instead of a per-type
  matrix; it degrades gracefully at any bucket count and gives the app one
  consistent "the faint line is the baseline" language.

## Considered Options

- **Per-Panel comparison:** rejected for now — a new per-Panel config axis and
  more UI, breaking the clean time-axis/metric-axis split; addable later as an
  override.
- **`previous`-only baseline:** rejected — misses "same period last year," the
  one genuinely distinct comparison health users want (annual weight change,
  seasonal activity), and custom spans.
- **Client orchestrates two requests:** rejected — pushes calendar/baseline-rule
  math into the client and risks the two windows disagreeing on truncation or
  "same period last year."
- **Calendar-date alignment / keep trailing baseline buckets:** rejected —
  cannot overlay differing dates on one axis; orphan buckets have no counterpart
  to compare against.
- **Headline or per-bucket delta in this milestone:** deferred — ill-defined for
  derived Metrics; belongs to the trends/summary lane.
- **Per-chart-type overlay (grouped bars for bar panels):** deferred — prettier
  for the narrow 7-day-bars case but an N-case matrix with a bucket-count
  ceiling; polish, not MVP.
- **Ephemeral client-only comparison toggle:** rejected — the time axis is
  already persisted; a saved reading (e.g. "weight, this month vs last year")
  should survive a reload like the range does.

## Consequences

- Migration adds `baseline_rule`/`baseline_from`/`baseline_to` to `dashboards`,
  validated like the range columns; the Dashboard DAO, API payload, and
  validator carry the new fields.
- The query engine gains a baseline-window resolver (rule → concrete window,
  calendar-aware) and an ordinal truncate-to-shorter aligner, reused by
  `/v1/series`; the derived path is untouched (a derived baseline resolves each
  operand's own Source per ADR 0003, exactly as the current window does).
- `/v1/series` returns a current series and an optional baseline series, equal
  length and index-aligned, each baseline bucket carrying its real date.
- An empty baseline window renders no baseline line (gaps, never zero —
  consistent with existing gap semantics). A `latest` Metric compares
  bucket-for-bucket with no special handling.
- The range `all` disables the comparison control in the SPA.
