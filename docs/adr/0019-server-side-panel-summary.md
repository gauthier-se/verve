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
