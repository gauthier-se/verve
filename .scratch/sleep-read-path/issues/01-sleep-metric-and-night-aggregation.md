Status: done

# 01: catalog, query: sleep is a Metric, aggregated at Night grain

## What

- **Catalog**: add `{"sleep", "min", DurationByState}` to the `rows` table in
  `buildMetrics` (`internal/catalog/catalog.go`). Drop the "unused in this
  slice" tail from the `DurationByState` comment; it is now the rule of one
  entry.

- **`internal/query/sleep.go`** (new file, so `query.go` does not grow a second
  storage backend inline). It owns everything that reads `states`:

  - `nightExpr` is the bucket expression, the Night's SQL twin of
    `Bucket.sqlExpr`:
    ```sql
    -- Day:   date(start_at, '+12 hours')
    -- Week:  date(start_at, '+12 hours', '-6 days', 'weekday 1')
    -- Month: date(start_at, '+12 hours', 'start of month')
    ```
    The `+12 hours` shift is applied first and once; the week/month snapping is
    then the existing expression applied to the shifted instant, so a Night
    never lands in a different week than its own label.

  - `seriesSleep(ctx, req, metric) (Series, error)`, called from `Series` before
    the `resolveSource`/`aggregate` path (which is measurement-shaped and stays
    untouched):

    1. Select the window's intervals: `account_id`, `kind = 'sleep'`,
       `start_at >= ? AND start_at < ?` over the *shifted* window — the window
       bounds are compared against `start_at` as everywhere else, so a night
       beginning before `From` belongs to the previous Night and is out, which
       is the same edge every bucket already has.
    2. Resolve per Night, in Go, over the ordered rows: group by Night; within a
       Night, if any row's `state_value` starts with `asleep` keep only rows
       whose Source is the best-ranked Source *that has such a row* and drop the
       `in_bed` rows; otherwise keep the `in_bed` rows of the best-ranked Source
       present. Reuse `catalog.ResolveSource` for the ranking so sleep does not
       invent a second priority mechanism.
    3. Sum `end_at - start_at` per (Night, Stage) in minutes, clipping each
       interval to the requested window so a partly-out-of-range night reports
       only the minutes inside it.
    4. Emit one `Point` per Night with data, ordered by bucket:
       `Value` = the sum of the `asleep*` Stages, `States` = every Stage kept
       including `awake` (and including `in_bed` when it is the Night's only
       Stage).

  - `summarizeSleep` folds the same resolved rows over the whole window as a
    single bucket (ADR 0019 unchanged): one `Point` whose `Value` is total time
    asleep and whose `States` are the window's per-Stage totals. A window with
    no data is a nil gap, never a zero.

  Reading the window's rows into Go rather than doing the resolution in SQL is
  deliberate: "the richest evidence wins, per Night" is a rule about a *group*,
  and expressing it as a correlated subquery would be unreadable for no gain —
  `maxPoints` already caps a query at 1000 buckets, and a night is tens of rows.

- **`Point.States map[string]float64 \`json:"states,omitempty"\`** with a comment
  tying it to the Stage vocabulary. `omitempty` means every existing Metric's
  JSON is byte-identical to today; assert that in a test.

- **`Series.Nights int \`json:"nights,omitempty"\`** — the count of Nights with
  data — set only on the sleep path, documented in the same register as `Mean`
  ("set only for a `latest` Metric"): the honest denominator for a per-night
  figure, so the client divides a server total by a server count.

- **`aggregate`'s default branch** keeps returning `ErrUnsupportedAggregation`,
  but its comment is now wrong twice over — rewrite it: `duration_by_state` is
  served by `seriesSleep` and never reaches here.

- **Tests** in `internal/query/sleep_test.go`, following the shape of
  `manualoverlay_test.go`:
  - a night spanning midnight lands in one bucket, labelled by its waking day;
  - a night fragmented into six rows across midnight lands wholly in that same
    bucket (the regression that kills the attribute-by-end-instant rule);
  - `value` excludes `awake`, `states` includes it;
  - a Night with Watch stages and iPhone `in_bed` counts the stages only, and
    the very next Night with `in_bed` alone counts it — one series, both rules,
    which is the case whole-window ranking gets wrong;
  - week and month buckets sum their Nights, and a Night never falls in a
    different week than its label;
  - an interval half outside the window contributes only its inside minutes;
  - an empty window is an empty non-nil `Points`, empty `Source`, nil summary;
  - `Nights` counts nights with data, not calendar days;
  - cross-Account isolation: another Account's states are invisible.
  - a `Compare` over sleep produces an ordinally aligned Baseline with no extra
    work (a test, not a change).

- **Docs**: ADR `docs/adr/0027-sleep-is-a-metric-at-night-grain.md` and the
  **Night** / **Stage** entries in CONTEXT.md, per the PRD's Docs section.

## Why here

The pull is to give sleep its own everything — its own endpoint, its own page,
its own notion of a range — because it is the first non-scalar family to be
read. The cost of that is a second implementation of the time axis, the
Baseline, the summary and the Panel, and a permanent second answer to every
question ADR 0012, 0015, 0019 and 0020 already settled once. Making sleep a
Metric with a richer Point costs one new file and one new field, and everything
downstream stops being a decision.

The Night is where the honesty of this milestone lives. Attributing an interval
to `date(start_at)` is the obvious implementation and it is wrong in a way that
looks right in a unit test with one tidy interval per night: real Apple data is
fragmented into short rows, and the 02:00 rows would file themselves under the
following day while the 23:40 rows stayed behind. Hence the shift-then-date
expression, and hence the fragmented-night test standing guard over it.

## Comments

Three departures from the spec above, each found while writing it out.

**One night expression, not three.** The spec had a `nightExpr` per bucket. The
week and month forms are unnecessary and slightly dangerous: folding the
resolved Nights in Go from their own labels makes "a Night is never in a
different week than its label" true by construction rather than by a modifier
order nobody will re-derive in a year. SQL now has the day form only.

**The window predicate is on the Night, not on `start_at`, and nothing is
clipped.** The spec said to clip each interval to the window. That is the
measurement rule applied to intervals, and it cuts the last night of every
window in half: a range ending at midnight would report a three-hour night that
was eight. A Night belongs to a window whole or not at all. The `start_at`
bounds survive only as index pruning, a day of slack either side.

**A Night whose only evidence is `in_bed` counts it as its value.** The spec had
`value` = the `asleep*` Stages, full stop, which gives an iPhone-only Account a
chart of zero-height bars and a summary of nothing — a wrong answer dressed as
an empty one. The value is time asleep, or time in bed when that is all that was
recorded, computed per Night and summed per bucket. The consequence is recorded
in the ADR: in a week bucket mixing a staged Night with an in-bed-only one, the
stacked segments sum to more than the bar.

Two smaller things: `Point` stopped being comparable with `==`, so the ~20
existing `!=` assertions became a `samePoint` helper (no assertion changed);
and `sleep` gained a Source priority entry (`watch` over `iphone`) to break the
tie between two Sources that staged the *same* night, which alphabetical order
was deciding by accident.

Verified by mutation: removing the `+12 hours` shift fails six of the ten sleep
tests. `make ci` is green.
