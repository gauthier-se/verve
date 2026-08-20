# Sleep is a Metric, and its bucket is the Night

## Context

Sleep has been imported since the core milestone. Every stage Apple records
lands in `states` as one row with a start, an end and a Source, and nothing has
ever read them: the query engine aggregates `measurements` and rejects the
`duration_by_state` rule outright, no endpoint serves a State, no screen shows
one. It is the largest gap between what a Verve database holds and what its
owner can see.

Reading it raises two questions that have nothing to do with each other, and
both have an obvious answer that is wrong.

**Where does sleep live in the read model?** The obvious answer is that States
are a distinct family (ADR 0001), so they get a distinct read path: a
`/v1/sleep` endpoint and a sleep page. The cost is not the endpoint, it is
everything the endpoint would have to grow afterwards — a Time range, a bucket,
a Baseline, a summary, a comparison — all of which already exist, are already
resolved server-side from stored tokens (ADR 0015), and would then exist twice,
answering the same questions in two places that can disagree.

**What is a bucket of sleep?** The obvious answer is the calendar day, like
every other Metric. But a Measurement is an instant and a sleep interval is a
span that crosses midnight by construction, so `date(start_at)` splits a night
in two and calls neither half a night. Worse, it does not split it cleanly:
Apple records a night as dozens of short rows, so the rows before midnight file
themselves under one day and the rows after under the next, and both days are
wrong by an arbitrary amount that varies with when the person fell asleep.

A third question follows from the second. Sleep is the case where two Sources
are *complementary* rather than competing: the Watch has the stages, the iPhone
has the nights the Watch spent on a charger, and the iPhone also records `in_bed`
over the very same minutes the Watch was recording stages in. Whole-range Source
priority (ADR 0003) either double-counts the staged nights or deletes the
unstaged ones.

## Decision

**Sleep is a Catalog Metric.** `{"sleep", "min", duration_by_state}`, served by
the existing `GET /v1/series` like every other Metric, with the per-Stage
breakdown carried on each Point as `states`. Its read path is a separate file in
the query engine (`internal/query/sleep.go`) because its storage is different;
its *shape* is a `Series`, identical to every other, because everything
downstream of the engine must not care.

Everything a Metric already has, sleep has, with no second implementation: the
resolved time axis, the Baseline and its ordinal alignment, Panels and
cross-metric composition, the Panel summary, Pins, the Metric page, the Ledger.

**The bucket is the Night: the noon-anchored day an interval falls in, keyed on
the morning it wakes into** — `date(start_at, '+12 hours')`. Both halves of a
night land in the same bucket however finely it is fragmented, and the label is
the waking day, which is the day the rest of the Dashboard is talking about
(last night's sleep against this morning's resting heart rate is the comparison
people actually make).

A Night belongs to a window whole or not at all: the range predicate is on the
Night, not on `start_at`. Week and month buckets are folded from the Night
labels rather than from the intervals, so a Night can never fall in a different
week than the day it is named after.

**A Night's evidence is resolved per Night, richest first.** A Night with any
staged row drops its `in_bed` rows; a Night with none keeps `in_bed` as its
single Stage. The Source is elected per Night among those that staged it, using
the standing Source priority to break the tie, rather than once for the whole
range. This is a deliberate, scoped departure from ADR 0003, which resolves a
winner per range: it holds where a device produces a continuous stream, and
sleep is not that. The grain is the Night for the same reason the Manual
overlay's grain is the day (ADR 0022): it is the grain at which the evidence
actually changes.

**A Point's value is time asleep** — the sum of the `asleep*` Stages — **or time
in bed when that is all that was recorded.** `awake` is always reported in the
breakdown, so the stack shows the interruptions, and never counted as sleep.

**The Panel summary rule is unchanged** (ADR 0019): one bucket spanning the
range. What is added beside it is `Series.Nights`, the count of Nights holding
data, which is to a per-night figure what `Days` is to a per-day one. A 30-day
window over 21 recorded nights divides by 21; dividing by 30 would report a
shortfall the Account never had, with the confidence of a computed number.

## Considered Options

- **A Metric, bucketed by Night (chosen).** One new file in the engine, one new
  field on the Point, one on the Series. Every downstream mechanic is untouched.
- **A `/v1/sleep` endpoint and a sleep page.** Faithful to States being their
  own family, and it duplicates range resolution, comparison, summaries and
  panel layout for exactly one family — permanently, since the second copy would
  then have to be kept in step with the first. Rejected.
- **Bucketing by `date(start_at)`, the calendar day.** Every other Metric's rule,
  and it splits a fragmented night across two days with the split point
  depending on when sleep began. Rejected; a test seeds a four-fragment night
  across midnight and fails if the shift is removed.
- **Splitting an interval at the bucket boundary and counting the overlap in
  each.** Mathematically clean, aggregates correctly at every grain, and reports
  two half-nights that nobody slept. Rejected.
- **Attributing an interval by its end instant.** Correct for one tidy interval
  per night, wrong for real data: it scatters a fragmented night across two
  buckets exactly as `date(start_at)` does. Rejected.
- **An 18:00 anchor rather than noon.** Keeps a late-afternoon nap on its own
  day, which noon pushes to the next. Both anchors are offset-sensitive and the
  engine dates in UTC, so neither is right for every timezone; noon is the
  larger, more explicable margin and the conventional one. Revisit if an Account
  timezone is ever stored.
- **Whole-range Source priority, as for measurements.** Cheap, reuses the
  existing filter, and either doubles the staged nights or deletes the ones the
  winner's device missed. Rejected.
- **Counting `in_bed` beside the stages.** Doubles every staged night, since it
  is the container the stages sit inside. Rejected.
- **Excluding `in_bed` unconditionally.** Consistent, and reports zero sleep to
  every Account that tracks nights with an iPhone alone: a wrong answer dressed
  as an empty one. Rejected.
- **A per-night mean as the summary, special-cased in the engine.** It is the
  figure a person wants, and it would make sleep the one Metric whose summary
  does not follow ADR 0019. Serving the total with an honest denominator gives
  the client the same figure without breaking the rule. Rejected.

## Consequences

- `Point` is no longer comparable with `==`, because the per-Stage breakdown is
  a map. A map is the honest model — `states.state_value` is free text and the
  closed set of Stages lives in the Connector, not in the engine — and the cost
  is paid once, in a `samePoint` test helper.
- Sleep appears wherever a Metric appears, including places nobody has to build:
  the add-panel dialog, the Ledger, the Pin toggle, `GET /v1/metrics`.
- Two things become reachable that must not be: a typed sleep Measurement, which
  would land in `measurements` where this read path never looks, and a
  `duration_by_state` operand inside a derived Metric's Formula, which would
  fold time asleep into a ratio with no meaning. Both are refused — the first at
  the API boundary, the second at Catalog build time.
- In a week or month bucket mixing a staged Night with an in-bed-only one, the
  stacked segments add up to more than the bar's value. It is the only case
  where they can, and it is a consequence of in-bed time and staged time
  overlapping by nature.
- The `stand` States stay unread. The same aggregation would serve them, and a
  stand hour is a flag rather than a duration; `apple_stand_time` already
  answers the question in minutes as an ordinary Measurement.
- A hypnogram — one night's stages on a time-of-day axis — is still not
  possible, and not because of this decision: the API caps resolution at the day
  (ADR 0012). It needs an intra-day read path, which is its own decision.
- Sleep onset, wake time and sleep efficiency are now one step away rather than
  blocked, but none of them is expressible as a Formula over Metrics (ADR 0014),
  so each needs its own answer.
