# PRD: a body-mass chart draws the noise and hides the signal

## Goal

A daily body mass is a noisy sample of a slow quantity. Hydration, glycogen, gut
content and the hour of weighing move a morning reading by more than a kilogram,
while the thing the Account is steering by moves by a tenth of one. Verve draws the
readings and nothing else, so the Panel shows mostly the part that does not matter.

On the reference Account over 15 July – 27 August 2026:

| Reading | Value |
| --- | --- |
| First reading minus last reading | **−4.85 kg** |
| Least-squares trend over the same window | **−3.96 kg** (−92 g/day, −0.65 kg/week, r² 0.86) |

The endpoints overstate the loss by 0.89 kg — 22% — because 15 July happened to be a
high morning. Every question the Account actually has ("am I still losing", "how
fast", "did the last two weeks stall") is a question about the second row, and the
chart answers only the first.

**Verve already computes this.** `ordinaryLeastSquares` lives in
`internal/estimate/estimate.go`, and `ActualRate` already returns kg/week and
%/week from it. The slope is reachable in exactly one place — a number on the Plan
page — and is absent from the chart of the Metric it describes. What is missing is
not the mathematics. It is the line.

## The concept

**A trend is a reading of the Metric, not an Estimate of another one.** ADR 0023
keeps Estimates out of the Catalog because an Estimate answers "what is true" about
a quantity the Metric only approximates — an expenditure is not a measurement. A
trend makes no such move: it is the same quantity, in the same unit, with the noise
taken out. So it belongs on the **Series**, beside `summary` and `mean`, and not in
`internal/estimate`.

**Server-computed, never re-derived client-side.** The rule `summary`, `days` and
`mean` already follow, and for the same reason: two clients smoothing independently
would disagree about a number the Account reads as one fact.

**`Latest` Metrics only, in v1.** `body_mass`, `body_fat_percentage`,
`lean_body_mass`, `body_mass_index`. These are sampled quantities whose bucket value
is one reading rather than a fold, which is exactly the shape a trend is for. A
`sum` Metric's bucket is already an aggregate of everything in it, and smoothing a
weekly step total is a different feature with a different argument. Nothing here
forecloses it.

**Two different smoothings, deliberately.** This is the decision worth arguing:

- **The drawn line is an exponentially weighted moving average.** The Account
  chooses the window, and over a year body mass is not a straight line — a
  least-squares fit across a cut, a holiday and a rebuild draws one line through
  three different stories and describes none of them. An EWMA follows the curve.
- **The quoted rate is the least-squares slope over the window**, which is what
  `ActualRate` already returns. Differencing an EWMA's endpoints to get a rate
  reintroduces precisely the endpoint sensitivity the ADR 0023 comment rejects
  ("never from differencing the endpoints: raw weight swings by ±1.5 kg between
  mornings").

The tension is real and should be stated rather than hidden: the line you see and
the rate you read come from two estimators, so over a curved window the line's
visible slope at the right-hand edge will not equal the quoted rate. That is
correct — they answer different questions, "where am I" and "how fast over this
whole window" — and the UI has to say which is which rather than let the Account
assume they are the same number.

**r² travels with the rate.** A slope of −92 g/day at r² 0.86 is a description; the
same slope at r² 0.05 is a line drawn through a cloud. Carry it and show it. Do not
gate on it: hiding a weak trend teaches the Account that a trend is always
trustworthy, which is worse than showing a weak one next to the number that says so.

**A trend breaks where the history breaks.** ADR 0032 draws a gap because the
history is dense enough for absence to mean something. A smoothed line bridging two
months of silence would assert a path nobody weighed. The EWMA is computed over days
that carry a reading and the line segments across the same gaps the series does.

**Exclusions and the Manual overlay come for free**, and this is the argument for
computing it in the query engine rather than anywhere else: the trend is fitted to
whatever `resolveSource` returned, so a corrected day corrects the trend and an
excluded Source never enters it. Fitting it in a second place would be a second
answer to "which rows count".

## Considered and rejected

- **Draw it client-side from `points`.** Cheapest, and it breaks the one rule the
  wire contract is explicit about. It also silently gets a different answer than the
  Plan page, which fits server-side over its own window.
- **Least squares for the line too.** One estimator instead of two, and it
  misdescribes any window longer than a phase. Kept for the rate, where the window
  is the question being asked.
- **EWMA for the rate too.** Self-consistent with the line and endpoint-sensitive by
  construction — the failure ADR 0023 already ruled out for this exact quantity.
- **Make the trend a derived Metric (ADR 0014).** A Formula is a ratio of weighted
  sums and cannot express a recurrence over time. Widening it to host one would
  trade a small closed language for an open one, the same trade ADR 0023 refused for
  Mifflin-St Jeor.
