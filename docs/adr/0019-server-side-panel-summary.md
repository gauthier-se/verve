# Server-side Panel summary

Every Panel gains a **Panel summary**: a headline figure above the curve so
magnitude — not only the shape of the trend — is readable at a glance, addressing
that a curve alone never shows a total or a mean. The summary is the Metric folded
over the whole Time range, and it is computed **server-side** and returned on the
`Series` (a new `summary` field), never re-derived in the client.

## Considered Options

- **Server-side (chosen).** The summary is defined as *a single bucket spanning the
  range*, so the read engine already knows how to produce it: an `average` becomes a
  true count-weighted mean over the raw data, a derived Metric aggregates each
  operand over the window then applies its Formula once, and an all-empty window is a
  gap, reusing the ADR 0014 gap rule unchanged. Correct everywhere, and consistent
  with ADR 0012 (the client never re-aggregates). The Baseline carries its own
  summary too, so the comparison **delta** is exact.
- **Client-side folding of buckets.** Free and needs no API change, but folding
  bucket values is *wrong* for `average` — a mean of per-bucket means is biased,
  since buckets carry unequal underlying sample counts — and it violates ADR 0012.
  Rejected.

## Consequences

- The `Series` contract grows a `summary` field (and the Baseline series likewise),
  computed in `internal/query`. This is a versioned-API change (ADR 0005).
- The **secondary** figure (the most recent bucket's value) is *not* a summary — it
  is `points[last]`, a plain read — so it needs no server support.
- The delta is shown **only** in period comparison and is never colored by sign or
  direction, matching the Baseline's own uncolored treatment (ADR 0015). Percentage
  by default; absolute for a signed Metric (ADR 0014), where a percentage around zero
  is meaningless.
- The summary is universal and carries no per-Panel toggle, so a Panel never has to
  be configured into showing its number.
- **Average rendering (follow-up).** A global display preference (localStorage, like the
  theme — not per-Panel, not server state) can show each summary as its **period
  average** instead of the window total, since an average reads better for trends. Per
  Metric shape:
  - **extensive** (a `sum`, or a derived plain weighted sum with no denominator — total
    energy = active + basal, calorie balance) → **per-day average** (total ÷ window
    days), so "≈13 k steps/day" beats "94 k this week" and a derived total stays
    consistent with its per-day components;
  - **`latest`** (body mass) → the **window mean** of its readings, not the last one, so
    the trend (this period's mean vs the compared period's mean) is legible;
  - **intensive** (an `average` rate, a ratio) → unchanged; it is already a mean.

  Nothing is re-aggregated client-side: the total, its day count, and the mean all come
  from the server. The `Series` therefore carries `days` (the window's whole-day span,
  the per-day denominator — right even for a Baseline of a different length) and, for a
  `latest` Metric, `mean` (the window average). A second preference toggles compact
  ("94,1 k") vs exact ("94 100") numbers.
- **Legible delta.** In comparison the summary shows the **compared period's own figure**
  beside the delta ("↓ 18 % (vs 1 166,82)") — a bare percentage against an invisible
  reference was not understood. The arrow follows the *shown* magnitude, so a change that
  rounds to zero reads neutral ("→ 0 %") rather than a contradictory "↑ 0 %". Still never
  colored good/bad (ADR 0015).
