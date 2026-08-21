Status: done

# 03: data, api: the history band and its ledger

## What

- **`internal/data/history.go`**: `Sources` (per Source: first seen, last seen,
  row count, unioned over measurements and states), `Span` (the outer bound of
  every dated family), `ListImports`, `CountUnmapped`.
- **`GET /v1/history`**: one call for the page.
  - The band: the chosen Metric (default `body_mass`) over the Account's own
    span, at the grain that span deserves, served **dense** — one entry per
    bucket, empty ones carrying `gap: true` — plus the gap runs pre-grouped.
  - The Phases folded onto that same grid and clamped to it; an open Phase runs
    to the last drawn bucket.
  - The events: imports, phases, notes, sources and the origin, newest first,
    ties broken by kind. Typed, not written — the words are the interface's.

## Why

See ADR 0032. The band is dense because this is the one read where a gap is the
subject rather than a rule about drawing, and because the alternative is bucket
arithmetic in the client, whose disagreements with the server render nothing at
all rather than failing.

## Done when

- An Account with no data gets a page, no band, and no invented window.
- The band is one point per bucket with the gap runs named, both ends snapped to
  their day so a 15-day span is 15 buckets and not 16.
  `TestHistoryBandIsDenseAndNamesItsGaps`.
- A Phase arrives as bucket keys the band actually draws.
  `TestHistoryFoldsPhasesOntoTheGrid`.
- Every kind appears once, newest first, with its figures.
  `TestHistoryEventsGatherEveryDatedSource`.
