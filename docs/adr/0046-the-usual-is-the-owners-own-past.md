# The Usual is the owner's own past, and whole buckets only

## Context

"My resting heart rate is 58" says nothing. "58, when my recent days sit between
46 and 52" says everything. Verve draws the value and nothing to read it against,
except the Baseline (ADR 0015), which is one other window the Account has to
choose, drawn as one other line.

The two things already called a band are not this. `Point.Min`/`Max` is the spread
*inside* one bucket, the lowest and highest reading of a day. The Band (ADR 0032)
is the dense history, drawn with its gaps. What is missing is the spread *across*
buckets: where this day sits among the days before it.

Four questions had an obvious answer that is wrong:

- **Which days make up "usual"?** The obvious answer is the same weekday ("my
  Tuesdays"). Measured on the reference Account over eight weeks, a per-weekday
  p25 to p75 rests on seven or eight values, and for resting heart rate and HRV
  the difference between weekdays is mostly the noise of that small n: Tuesday
  43.8 to 50.8, Wednesday 46.2 to 49, against 46 to 52 for all days pooled. Only
  steps have a real weekly rhythm.
- **One band, or one per bucket?** The obvious answer is one band from the last N
  weeks, drawn across the chart. Across a long history it would read 2022 against
  2026, which is the Baseline's job.
- **Is the bucket in its own reference?** The obvious answer is that it doesn't
  matter much. It does: a value read against a set that contains it is pulled
  towards looking usual.
- **What about a bucket the window cuts?** A window starting on a Wednesday draws
  its first week from five days. Read against whole weeks, a week summed over
  five days reads as a collapse that never happened, and reused as reference it
  drags down the weeks after it.

## Decision

**The Usual of a bucket is the p25 to p75 of the Metric's own bucket values over
a trailing window that ends strictly before it**, pooled over every day, not per
weekday. Quartiles are by linear interpolation between closest ranks (Hyndman and
Fan type 7, the default of numpy and of `PERCENTILE.INC`), so two implementations
cannot disagree. N, the number of values behind the band, travels with it.

**Day and week grain only.**

| Grain | Window | Minimum values |
| --- | --- | --- |
| Day | the 28 preceding days | 14 |
| Week | the 12 preceding weeks | 8 |

Twenty-eight days hold four of every weekday, so pooling them overweights none,
and a new habit becomes the usual within a month. Twelve weeks is a season. Below
the minimum the bucket carries no Usual. A month has none: it would need most of a
year to reach a usable count, and would then describe a year.

**A gap is absent, never a zero** (ADR 0014), and counts against the minimum.

**Whole buckets only.** A bucket the request window only partly covers carries no
Usual, whatever the Metric. The reference read runs to the end of the bucket
holding `From`, so a week the window cut enters later references whole. The API
also clears the Usual of the bucket holding now, which a custom range can reach:
a day in progress has not fallen short of its past, as a Goal never judges today
(ADR 0044).

**Computed in the read engine, on the resolved values**, after the Source election
(ADR 0034), the Manual overlay (ADR 0022) and Exclusions (ADR 0033), from the same
buckets the chart draws. It covers every nature, derived Metrics and Nights
included, because it reads `Point.Value` from the one `series` path.

**Opt-in on the Request.** The reference reaches before `From`, which costs a
second, short read (28 days or 12 weeks). Only `/v1/series` asks for it, and only
for a lone Metric: a multi-Metric Panel draws no band (two bands on two axes read
as nothing, the argument of ADR 0020), and `Compare` does not ask for the
Baseline's, since in comparison the Baseline line is already the reference. The
engine's own callers (a Goal's day buckets, a Co-variation, the Ledger) never pay
for it.

**Descriptive, never a verdict.** The UI may say "above", "within" or "below your
usual". It never colours a point, never calls it high or low, and never counts
days outside the band: counting belongs to a Goal, which the owner declared.

## Why

- **It meets the refusal to interpret instead of bending it.** There is no
  external norm and no reference population. The only claim is "this is where
  your own recent values sat", which is a fact about the history.
- **A strict past is what makes it readable.** "Above my usual" only means
  something if the usual was there before the value.
- **Pooling is the honest default.** A band that moves by a point every time one
  value is added teaches a reader to read noise. Weekday conditioning can come
  back per Metric if a weekly rhythm turns out to matter, with the n to carry it.
- **One read path.** A Usual computed from anything but the engine's resolved
  buckets would be a second definition of a day's value.

## Considered Options

- **Per weekday.** Deferred, with the measurement above. Revisit for `sum`
  Metrics with a weekly rhythm (steps) if the pooled band hides one.
- **Per-rule conditioning** (weekday for `sum`, pooled otherwise). Rejected for
  v1: one more rule to explain for a gain limited to a handful of Metrics.
- **A fixed band over the last N weeks.** Rejected: it compares the whole history
  to the present.
- **Computing it for every Series.** Rejected: it doubles reads for thirteen
  internal callers that never draw it.
- **Clearing only accumulating Metrics on a cut bucket.** Rejected: a partial
  week of sleep is summed too, and one rule for every Metric is easier to trust.
- **Mean plus or minus one standard deviation.** Rejected: one bad night moves
  it, and it assumes a symmetry that sleep and steps do not have.
