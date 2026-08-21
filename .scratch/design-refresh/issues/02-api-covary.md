Status: done

# 02: query, api: co-variation over the Pins

## What

- **`internal/query/covary.go`**: `CoVary(ctx, CoVaryRequest)` reads each Metric
  once over the window, keys its buckets by start date, and pairs them.
  Spearman's ρ (`ranks` with average ties, then Pearson on the ranks); a
  least-squares `fitLine` for the scatter; `thresholdShared` at 60 % of the
  window's buckets with a floor of 8. Pairs come back sorted strongest-first with
  unranked ones below every ranked one.
- **`GET /v1/covary`**: reads the Account's Pins (capped at 8, dropped from the
  tail in Pin order), resolves the window through `timeaxis`, and answers the
  whole page — pairs, ranking, threshold, the strongest pair drawn. `lag` is one
  of `same` / `next_day` / `next_week`, each carrying both the shift and the
  grain it is asked at.
- Pinned Metrics that could not join are **named** with a reason, never silently
  dropped.

## Why

See ADR 0031. The short version: this is the screen where a health app usually
stops being honest, so the measure is robust to the outliers this data actually
has, the evidence count travels beside every coefficient, and a pair with too
little overlap says so rather than reading as a weak result.

## Done when

- ρ is 1 on a monotone non-linear pair and 0 when one side never moves — never
  NaN. `TestSpearmanIsMonotoneNotLinear`, `TestSpearmanFlatSideIsZero`.
- A lag shifts the *second* Metric, and the two directions of a pair find
  different overlaps. `TestCoVaryLagShiftsTheSecondMetric`.
- An under-threshold pair is reported, flagged and sorted last, and no scatter is
  drawn. `TestCoVaryUnrankedPairsSinkButStay`.
- The endpoint reads the Pins and nothing else, `next_day` forces day grain over
  a window that would bucket weekly, and a skipped Pin carries a reason.
  `internal/api/covaryhandlers_test.go`.
