Status: done

# 01: query, api: the reading count and the resolved time axis

## What

- **`query.Point.Count`**: how many rows a bucket was folded from — Measurements
  for an observed Metric, Nights for a `duration_by_state` one. Added to the
  per-bucket SQL for all three rules (`sum`, `average` by `COUNT(*)`; `latest` by
  `COUNT(*) OVER (PARTITION BY bucket)`, so the count is the bucket's whole row
  set and not the one row that won) and to the window summary. Zero, and omitted
  from the JSON, for a derived Metric: its operands each have their own count and
  a combined one would name no row set.
- **`GET /v1/timeaxis`**: the same `timeaxis.Resolve` every Series query runs,
  exposed on its own. Answers the current window (`from`, `to`, `last`, `days`),
  the bucket, and the Baseline window when comparison is on. Reads no Account
  data but stays behind the session like every other `/v1` route.

## Why

Both exist so the interface can state a fact instead of deriving one.

The Ledger's "Readings" column and the Metric page's "Readings" card are in the
design, and without `Count` there is no honest number to put in them — an average
of 52 over three hundred readings and an average of 52 over two are the same
number and not the same fact.

`/v1/timeaxis` is the same argument about dates. A "1y" range resolved in the
browser, with the browser's clock and zone, produces a label that disagrees with
the buckets beside it near midnight and twice a year. One module owns the
boundaries (ADR 0012); this is the door onto it.

## Done when

- A `sum`, `average` and `latest` Metric each report a per-bucket and a window
  count; a derived Metric reports none. `TestSeriesCountsTheRowsBehindEachBucket`,
  `TestSeriesDerivedCarriesNoCount`.
- `/v1/timeaxis` resolves each preset to its grain, names `last` as the day before
  the half-open bound, refuses a comparison over `all` (ADR 0015) with a 422, and
  rejects an unknown preset. `internal/api/timeaxishandlers_test.go`.
