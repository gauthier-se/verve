# Multi-Metric Panels on dual axes

A Panel gains the roadmap's **cross-metric overlays** (v1.2): it carries one to
four Metrics, each with its own chart type, rendered as a combo chart on **up to
two Y axes** — Metrics sharing a unit share an axis, and the first Metric's unit
group takes the left. No new domain concept is introduced: the Panel simply
becomes what the glossary already said ("one or more Metrics"), and "overlay"
stays a rendering word.

## Considered Options

- **Dual Y axes, ≤ 2 units, cap 4 Metrics (chosen).** Every curve keeps its true
  scale and unit, consistent with the Panel summary's founding principle that
  magnitude must stay legible (ADR 0019). The real constraint is the number of
  axes, not of curves — one rule, no special case — and the cap protects
  readability and the categorical palette. Enforced server-side at Panel save.
- **Normalization (index 100 / min-max).** Unbounded Metric count and ideal for
  co-variation, but it erases magnitude — frontally contradicting ADR 0019.
  Rejected.
- **Small multiples.** Maximum readability, zero scale problems, but it is not
  an overlay (no shared vertical reading) and consumes grid height. Rejected as
  the *default*; nothing prevents a user from arranging single-Metric Panels.

## Consequences

- **Baseline is cut on multi-Metric Panels**: when period comparison is on, a
  Panel with more than one Metric renders no Baseline. Co-variation between
  Metrics and comparison between periods answer different questions; stacking
  them (up to 8 curves) is unreadable. Same exclusion mechanic as the `all`
  Time range (ADR 0015), decided server-side.
- **Chart type moves onto each Metric of the Panel**, defaulted by its
  aggregation rule exactly as today (sum→bar, average→line, latest→line…);
  bar + line combos are rendered with a composed chart.
- **Panel summary becomes per-Series in the legend** on multi-Metric Panels:
  each legend entry carries its Series' folded value. The summary stays
  universal (ADR 0019) — the server already computes it per Series; only the
  single-Metric rendering keeps the large headline figure.
- **Storage**: Panel→Metric becomes a join table (`panel_metrics`) with
  per-Metric chart type and position; the axis side is *not* stored — it is
  derived from the unit, and derivable data that is stored ends up lying.
- **API**: `/v1/series` accepts a repeatable `metric` parameter and returns one
  Series per Metric; the time axis is resolved **once** server-side so all
  Series share provably identical buckets — the same argument that put Baseline
  alignment server-side (ADR 0015). A single `metric` keeps today's behavior.
