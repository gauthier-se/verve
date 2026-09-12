Status: ready-for-agent

# 01: catalog, query: two Metrics folded from Sessions, capped at the ramp

## What

- **`catalog.SumByState`** in `catalog.go:16`'s Aggregation block, beside
  `DurationByState`, commented for what distinguishes them: the existing rule
  names minutes per Stage and is sleep's alone; this one is a summed quantity per
  breakdown key, in whatever unit the Metric declares. `catalog_test.go:39`'s
  `valid` map gains it, which is the test that would otherwise pass a typo.
- **Two Catalog rows**, in their own `--- Training ---` section next to Sleep's:
  ```go
  // Folded from the Sessions family rather than from measurements, broken down
  // by Activity, and capped at the ramp (training.go, ADR 0040).
  {"training_time", "min", SumByState},
  {"training_distance", "km", SumByState},
  ```
  Minutes rather than hours so the unit matches sleep's and the client's existing
  duration formatter reads it; kilometres because that is the canonical distance
  unit `sessions.total_distance` is already normalized to.
- **`internal/query/training.go`**, a sibling of `sleep.go` with the same opening
  comment shape: same Series out, different storage in.
  - `seriesTraining(ctx, req, metric) (Series, error)`, dispatched from
    `Series` (`query.go:158`) on `metric.Aggregation == catalog.SumByState`,
    beside the `DurationByState` branch and before everything measurement-shaped.
  - One read:
    ```sql
    SELECT date(start_at) AS day, activity_type, duration, total_distance, source
      FROM sessions
     WHERE account_id = ? AND start_at >= ? AND start_at < ?
     ORDER BY start_at
    ```
    `sessions_account_start` covers it. No noon shift: a Night straddles midnight
    by nature and a workout does not, so the day a workout started is its bucket
    and that is the whole rule.
  - The per-row quantity is the Metric's: `duration / 60` for `training_time`
    (the column is seconds, the unit is minutes), `total_distance` for
    `training_distance`, and a NULL distance contributes nothing while its minutes
    still count. A strength session has no kilometres and a zero would be a claim
    about a workout that happened.
  - Fold to the request's Bucket by reusing what sleep folds with (`foldNights`'s
    shape, `sleep.go`): day keys, then week and month in Go from the day labels,
    so a workout cannot land in a different week than its own label.
  - **The cap runs over the window, not the bucket**: total each Activity across
    the whole window, keep the five largest, and fold the rest into `other`. Per
    bucket it would make a segment mean "the biggest that week", which is a
    different quantity on every bar. Five plus `other` is six, the long ramp's
    length (ADR 0036) and a Night's cardinality.
  - `Point.Value` is the bucket's total across every Activity, capped or not, so
    the total never depends on the cap. `Point.Count` is the number of Sessions in
    the bucket, which is what "the reading count behind the bucket" means here and
    what the Ledger table's last column will show.
  - `Series.Source` reports the Sources read, through the same helper shape sleep
    uses. Nothing elects between them yet, and that is deliberate (see the PRD):
    the reported list is what will make a doubled figure visible the day a second
    Connector emits workouts.
  - `Series.Summary` is the window folded as one bucket, the same rule every
    Panel summary follows (ADR 0019), with its own breakdown so the legend can
    name the window's share per Activity.
- **`SessionModel.HasSessions(ctx, accountID) (bool, error)`** in `session.go`,
  the twin of `StateModel.HasStates` and for the same reason: the Ledger's row set
  comes from `DistinctMetrics` over `measurements`, which structurally cannot see
  a Metric read from another family.
- **The Formula validator** already refuses a `DurationByState` operand
  (`formula_test.go:76`); the check moves to "any by-state rule" so `SumByState`
  is covered by construction rather than by a second branch nobody updates.
- **Tests** in `internal/query/training_test.go`, mirroring `sleep_test.go`:
  - minutes and kilometres per day, two activities in one bucket;
  - a Session with a NULL distance: minutes counted, distance untouched, and the
    Point's value not zeroed;
  - week and month folding, including a workout on a Sunday and a Monday landing
    in different weeks;
  - the cap: seven activities in a window yield five named plus `other`, the
    total equals the uncapped sum, and the same Activity keeps its segment across
    every bucket;
  - a workout crossing midnight counts in the day it started;
  - the window bounds are half-open, asserted on a workout starting exactly at
    `To`;
  - cross-Account isolation, as every query test asserts;
  - `Summary` equals the window's total and carries its own breakdown.
- **Docs**: ADR 0040 and the **Training volume** entry in CONTEXT.md (see the
  PRD), plus the forward pointer on ADR 0028 and the sentence on **Activity**.

## Why here

The read is a sibling file rather than a branch inside `aggregate` because the
measurement engine's three concerns, the Source filter, the Manual overlay and
the per-bucket SQL, are all about measurements and none of them applies to a
workout. `sleep.go` made that call first and it has held; a second family
arriving is the test of it.

The cap belongs in the read and not in the client for the reason ADR 0002 gives
about Activity groups: a client that enumerates slugs to decide what to show
drops every Activity added after it shipped. It also has to be one decision over
the window rather than per bucket, or the legend stops describing the chart.

`Point.Value` counting the folded-away activities is the difference between a cap
that is a display choice and a cap that changes the answer. The total is the
Metric; the breakdown is how it is drawn.
