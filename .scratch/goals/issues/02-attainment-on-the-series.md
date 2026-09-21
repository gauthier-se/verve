Status: done
Blocked by: 01

# 02: goal, query, api: Attainment on the Series

## What

- **A read module, `internal/goal`**, with an `Engine{Query, Models}` like
  `internal/day` and `internal/history`: the read engine aggregates measurements
  and has no business naming the `goals` table (ADR 0037).

  ```go
  // Attainment is a Goal's three counts over a window (ADR 0044). A gap is neither
  // met nor missed, and today is never judged.
  type Attainment struct {
      Segments []Segment `json:"segments"` // the Goals in force over the window, oldest first
      Covered  int       `json:"covered"`  // days a Goal was in force, today excluded
      Measured int       `json:"measured"` // covered days with a value
      Met      int       `json:"met"`      // measured days within that day's bound
  }

  type Segment struct {
      Direction string  `json:"direction"`
      Value     float64 `json:"value"`
      From      string  `json:"from"`         // clipped to the window
      To        string  `json:"to,omitempty"` // exclusive, clipped; empty while open
  }
  ```

  Where the type lives is the implementer's call: `internal/goal` imports
  `internal/query`, so if `query.Series` carries the field, the type belongs in
  `query` as plain data that the engine never fills.

- **The counts come from `Engine.Series` at `timeaxis.Day`**, never from a
  second SQL path. That one call already covers imported, derived, sleep (by
  Night) and training Metrics, the Source election (ADR 0034), the Manual
  overlay and Exclusions (ADR 0033). A day's value is the Point for that day, so
  a Goal is judged against exactly the number the bar draws.

- **The day pass is chunked.** `maxPoints` (1000) refuses an `all` range at day
  grain, so the window is folded in chunks of at most `maxPoints` days, and only
  over the covered span: days before the first Goal cost nothing.

- **Today is excluded** from all three counts, using the day boundary the time
  axis already resolves (never the browser's clock, ADR 0012).

- **`/v1/series` attaches it.** Single series, multi-metric (one per Series) and
  comparison (the Baseline gets its own, against the Goals in force over its
  window). `nil` when no Goal touches the window. `/v1/series.csv` stays
  unchanged: the CSV is the data, not a judgement of it.

- **The contract** grows `goal?: Attainment` on `Series` in `types.ts`.

## Tests

- a Goal changed mid-window: each day is judged against its own segment, and the
  segments come back clipped to the window;
- a gap is counted neither measured nor met; a zero value is measured;
- `at_least` and `at_most` are both inclusive at the bound;
- today is excluded even when it has a value, including a `sleep` Night;
- a derived Metric with a missing operand on a day counts that day unmeasured;
- a weekly Panel window gets the same counts as the daily window over the same
  dates;
- an `all` range longer than `maxPoints` days is counted in full;
- a Baseline's Attainment uses the Goal in force during the Baseline, not the
  current one;
- an Excluded day is unmeasured, and the elected Source's value is the one
  judged.

## Why here

Computing the counts anywhere but from the engine's own day Points would be a
second definition of a day's value. The sleep read path (ADR 0027) and the Day
page (ADR 0043) both refused that, for the same reason.

## Comments

Shipped. `internal/goal/goal.go` with `goal_test.go`, `Attainment` and
`GoalSegment` in `internal/query/query.go` with `Series.Goal`, the four attach
points in `handleSeries` (single, multi-metric, comparison current and Baseline),
two handler tests in `goalhandlers_test.go`, and the `Attainment` and
`GoalSegment` types in `types.ts` pinned by the contract test.

What the implementation settled that the spec left open:

- **Excluding today was mostly already true.** Every preset window ends at
  today's UTC midnight, exclusive (`timeaxis.rangeWindow`), so today is outside
  it. Only a custom range can reach today or beyond, and `Attain` clips the
  judged span to `min(to, today)`. The segments are not clipped to today: the
  Goal is in force, it is just not judged yet, so the line still runs to the end
  of the window.
- **`GoalSegment.To` is always set**, clipped to the window, rather than empty
  while open. The client draws every segment the same way and never has to know
  where a window ends.
- **The chunk is a local 366 days**, not the engine's `maxPoints`, which stays
  unexported. Chunking cannot change a value, because the Source election runs
  per day (ADR 0034), and only the covered span is read.
- **The Baseline is counted over its whole window**, not the ordinally
  truncated one `alignOrdinal` draws. For `previous` and
  `same_period_last_year` the two are the same length. For a custom Baseline
  of a different length, the count covers the dates the owner asked for.
- **An error from `Attain` is a 500**, not a missing field: a series that
  silently drops its Goal would read as "no Goal set".
