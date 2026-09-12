Status: done
Blocked by: 01

# 02: api, data: what a by-state Metric cannot be asked, and where it shows up

## What

- **The Exclusion refusal covers both by-state rules.**
  `validateExclusionMetric` (`internal/api/exclusionhandlers.go`) refuses
  `sleep` with a 422 naming the reason: it is stored as States and this feature
  covers Measurements (ADR 0033). The predicate becomes "the Metric's rule is a
  by-state one" rather than an equality against `DurationByState`, so
  `training_time` and `training_distance` are refused by construction and with a
  message that says which family holds them. A slug accepted here and silently
  never excluded is the one outcome that feature was built to avoid.
- **The Manual entry refusal likewise.** `validateManualMetric`
  (`measurementhandlers.go`) refuses what it cannot write a Measurement for. Its
  message is its own, as issue 03 of the exclusions milestone explained: the two
  validators deliberately do not share a helper, because the sentences are the
  content. `training_time` gets one that says a workout is imported, not typed.
- **The Ledger's row set sees them.** `query.Ledger` builds from
  `MetricsWithData`, which reads `measurements`. Sleep is joined in by a
  `HasStates` probe; the two training Metrics join by `HasSessions` (issue 01),
  in the same place and the same way, so an Account with workouts and no
  `sleep` still gets both rows.
- **The Ledger's window figures are per bucket, not per day.** For a `sum`
  Metric the overview divides by the window's day count so steps read per day
  (`ledger.go`'s comment). A training Metric is a `sum` too, and dividing by
  every calendar day answers "minutes of training per day including rest days",
  which is the honest reading of a volume and the one the column header already
  implies. Keep the existing rule and say so in a comment, because the temptation
  to divide by active days instead is exactly the kind of silent per-Metric
  special case the Ledger exists to avoid.
- **The CSV is already right, and gets a test.** `csvRows`
  (`internal/api/exporthandlers.go`) emits one row per state for any Point that
  carries them, so `training_time` downloads as `training_time.running`,
  `training_time.cycling` and so on, at the bucket on screen. It costs nothing
  and it is worth pinning: the generalization from "sleep stages" to "any
  breakdown" happened in the export milestone by accident of good shape, and a
  test is what keeps it.
- **Co-variation leaves them out, as it leaves sleep in.** `CoVary` pairs pinned
  Metrics by their bucket values; a by-state Metric has a value, so nothing
  breaks. Confirm by test that a pinned `training_time` pairs on its total rather
  than on a breakdown, which is the only reading that has a meaning.
- **Tests**: the two 422s with their distinct messages, in
  `exclusionhandlers_test.go` and `measurementhandlers_test.go`; the Ledger row
  appearing for an Account with sessions and no measurements, in
  `ledgerhandlers_test.go` or `internal/query/ledger_test.go` where its siblings
  are; the CSV's per-Activity rows in `exporthandlers_test.go`; the co-variation
  pairing in `covary_test.go`.

## Why here

These are four places that each learned about `sleep` as a special case and will
each learn about a second by-state Metric badly if the check stays an equality.
Turning three of them into "is this rule a by-state one" is the whole issue, and
it is worth its own pass precisely because none of it is where the feature's
interest lies: it is where its silence would be.

The Ledger paragraph is a decision, not a note. A volume divided by active days
is a per-session average wearing a per-day label, and the Ledger's columns are
fixed by ADR 0021 so that one Metric cannot mean something different from its
neighbours.

## Comments

Shipped. The Exclusion and Manual-entry refusals, the default chart type and the
`stacked_bar` validation now ask `Aggregation.ByState()`; the Ledger's row set
probes the Sessions family; its per-day divisor treats a volume like any other
`sum`, with the comment saying why active days would be wrong. Tests: the two
422s in their existing tables, the row set in `ledger_test.go`, the per-day
figure, and the CSV's per-Activity rows in `exporthandlers_test.go`.

Two notes:

- **The CSV needed no code at all**, as predicted, and the test is the point: the
  export milestone generalized `csvRows` to "any breakdown" by accident of good
  shape, and training volume is the first Metric whose keys are an open set.
- **Co-variation needed nothing either.** It reads `Point.Value`, which is the
  bucket's total across every Activity, so a pinned `training_time` pairs on the
  only figure that has a meaning. No test was added where there is no branch.
