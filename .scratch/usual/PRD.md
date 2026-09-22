# PRD: a value read against the owner's own usual

## Goal

"My resting heart rate is 58" says nothing. "58, when my recent days sit between
46 and 52" says everything. Verve draws the value and nothing to read it against,
except the Baseline, which is one other window, drawn as one other line, that the
Account has to choose.

This milestone adds the **Usual**: for each bucket, the p25 to p75 of the Account's
own values over the buckets just before it, drawn as a band behind the curve.

It is purely descriptive. There is no external norm, no reference population, no
"healthy range". It compares the owner to themselves, so it does not conflict with
Verve's refusal to interpret (the change is never coloured good or bad, ADR 0015).
It is an evolution of the read engine, not a new page.

## What already exists, and why it is not this

- **`Band` (ADR 0032, `internal/query/band.go`)** is the dense history: one Point
  per bucket, gaps materialised, so absence can be drawn. It has nothing to do with
  a distribution.
- **`Point.Min` / `Point.Max`** are the intra-bucket amplitude of an `average`
  Metric: the lowest and highest reading *inside* one day. The Usual is the
  spread *across* buckets: where this day sits among the days before it.
- **The Baseline (ADR 0015)** overlays one chosen window, ordinal-aligned. The Usual
  needs no choice and moves with every bucket.

## Measured on the reference Account

Last 8 weeks to 24 July 2026, dominant Source, p25 to p75:

| Metric | All days (n about 55) | Per weekday (n = 7 or 8) |
| --- | --- | --- |
| Resting heart rate | 46 to 52 | Tue 43.8 to 50.8, Wed 46.2 to 49, Fri 50 to 58 |
| HRV (SDNN) | 70 to 85 | Mon 61.5 to 87.6, Thu 70.2 to 79.9 |
| Steps | 7.6k to 12.8k | Sat 3.8k to 15k, Sun 1.4k to 9.9k |

An interquartile range computed from eight values moves by a point every time one
value is added. Weekday conditioning, which was the first formulation ("my
Tuesdays"), mostly draws that noise. The weekday effect is real for steps, but a
band that differs by rule would be one more thing to explain. v1 pools.

## The concept

**The Usual is the p25 to p75 of the Metric's own bucket values over a trailing
window that ends strictly before the bucket.** The bucket never enters its own
reference: a value cannot be compared to a set that contains it.

**Rolling, not fixed.** Each bucket is read against the buckets before it, so the
band follows a change of habit, and a point from March 2024 reads against March
2024's usual rather than today's. A fixed band ("what is usual now") drawn across
a long history would compare 2022 to 2026, which is the Baseline's job.

**Day and week grain. Not month.**

| Grain | Window | Minimum values |
| --- | --- | --- |
| Day | the 28 preceding days | 14 |
| Week | the 12 preceding weeks | 8 |

28 days is four of every weekday, so pooling does not overweight any of them, and
it is short enough for a new habit to become the usual within a month. Twelve
weeks is a season. Below the minimum, the bucket carries no Usual: a quartile of
five values is not a description of anything. A month grain would need a year of
history to reach eight values and would describe a year, not a usual.

**Quartiles by linear interpolation between closest ranks** (Hyndman and Fan type
7, the default of numpy and of `PERCENTILE.INC`). Stated so two implementations
cannot disagree.

**N travels with the band**, as `Count` does with a bucket: a band from 14 values
reads differently from one from 28.

**A gap is neither in nor out.** Missing buckets in the window are simply absent
from the reference (never zeros, ADR 0014), and the minimum counts real values. A
bucket without a reading carries no Usual, so the band breaks where the series
does (ADR 0032).

**Whole buckets only.** A bucket the window only partly covers carries no Usual,
whatever the Metric: a week summed over three days is not comparable to whole
ones. The bucket holding today carries none either (a custom range can reach it),
because at 10:00 a step total is unfinished, not low, as a Goal never judges
today (ADR 0044).

**Every Metric with a scalar per bucket is eligible**: `sum`, `average`, `latest`,
`duration_by_state` (on time asleep, the Point's Value) and derived Metrics. The
Usual is computed on `Point.Value`, whatever the rule, which is the value drawn.

**Computed in the read engine, on the resolved values.** After Source resolution,
the Manual overlay (ADR 0022) and Exclusions (ADR 0033), for the same reason as the
trend: a second place would need its own answer to "which rows count". The reference
window reaches before the requested range, so the engine reads the prior buckets
itself; the client never asks for more history to compute it.

**Direction only, never a verdict.** The UI may say "above your usual" or "within
your usual". It never colours the point, never calls it high or low, never counts
days out of range. Counting belongs to a Goal, which the owner declared.

## Surfaces

- **Panels**, single-Metric, day or week grain: the band behind the curve, in a
  neutral fill that is not a slot of the ramp (it is not a Metric, ADR 0036/0042),
  and its bounds in the tooltip. Not drawn on a multi-Metric Panel (two bands on
  two axes read as nothing, the argument of ADR 0020) nor in comparison mode, where
  the Baseline line is already the reference.
- **Metric page**: the same chart.
- **Now**: each Pin card's Latest value followed by its usual, "58 bpm, usual 46 to
  52". This is the sentence the feature exists for.
- **Not**: the Ledger, the Day page, Cross-metric, the Band (history) view, any
  alert.

## Issues

1. `01-the-point-carries-its-usual.md`: query, api: the Usual on the Series, ADR
   0046 and the CONTEXT.md entry
2. `02-web-the-band-behind-the-curve.md`: web: the band on Panels and the Metric
   page
3. `03-now-reads-the-latest-against-its-usual.md`: now, web: the usual on each Pin
   card, and the docs pass

## Out of scope

- Weekday conditioning ("my Tuesdays"). Revisit per Metric if the pooled band turns
  out to hide a weekly rhythm people care about; the measurement above says it
  would be noise for heart rate and HRV.
- A window the Account chooses, or a per-Panel toggle.
- Any external or population range.
- Counting or streaking days outside the Usual.
- The Usual of a Baseline series.

## Docs

- ADR 0046 and the CONTEXT.md entry (**Usual**) land with `01`.
- `03` carries the README and the ROADMAP row.

## Vocabulary

**Usual** is the term. _Avoid_: Band (ADR 0032's dense history), Baseline (ADR
0015's second window), Normal range or Reference range (clinical connotation,
implies a norm), Typical, Range (the Time range and the Goal's future `between`
already claim it).
