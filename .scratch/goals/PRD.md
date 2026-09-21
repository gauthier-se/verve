# PRD: A Goal per Metric

## Goal

The only targets Verve knows are the Plan's, derived from a Target rate
(`planhandlers.go:142`). There is nothing for "7 500 steps", "7 h of sleep" or
"150 g of protein", which is the first thing a person expects of a health
dashboard: a line on the Panel, and how often it was met over the window.

Verve has refused this on principle: it does not colour a change good or bad,
because it cannot know which direction is good for a Metric. A **Goal** answers
that objection exactly. The owner declares the direction; Verve still judges
nothing, it counts. The shape is the same as a derived Metric's: declarative
data (Metric, direction, value, start date), not a rules engine.

This milestone adds the Goal, its history, its **Attainment**, and the places
it is shown. The decisions are recorded in ADR 0044 and the vocabulary in
CONTEXT.md (**Goal**, **Attainment**).

## The concept

**A Goal is judged at day grain, whatever the Panel's bucket.** "7 500 steps" is
a daily bound. The Metric's day bucket is the unit, which is the Night for
`sleep` (ADR 0027), so sleep needs no special case. A Goal per week is out of
scope.

**Two directions, inclusive.** `at_least` (≥) and `at_most` (≤). A range is a
future third direction, never two Goals on one Metric.

**Every Metric but `latest` is eligible, bounded on the Point's value.**
Eligibility is derived from the aggregation rule. Derived Metrics are eligible
and may take a negative value (`calorie_balance ≤ −300`). By-state Metrics are
bounded on their total, never on a State.

**A dated history.** One open Goal per (Account, Metric); a new one closes it;
each day is judged against the Goal in force that day. Bounds are dates
(`started_on`, `ended_on`, half-open), not instants. A new Goal must start after
the open one and after the last closed one; the past is corrected by deleting
an entry. A start date may be in the past (declaring "since January") but not in
the future.

**Attainment is three counts.** Covered, measured, met. A gap is neither met nor
missed. Today is never judged. Shown with its denominator, never as a bare
percentage.

**On the Series, server-side.** Attainment and the Goal segments over the window
travel on the Series, computed from the engine's own day buckets. A Baseline
carries its own, against the Goals in force over its window.

**Separate from the Plan.** Plan targets are derived on read and stored nowhere,
so no past day can be judged against them. The Panel draws declared Goals only.

**No valence.** A dashed step line in the Series' colour, on its axis, at day
grain only. No recoloured points, no shading, no tick, no streak, no
notification. The value axis extends to include the Goal.

## Surfaces

- **Metric page**: where a Goal is set, closed and deleted, with its history.
  Its chart carries the line and the counts.
- **Panels**: the line at day grain, the counts in the legend beside the Panel
  summary, the Baseline's counts beside them in comparison.
- **Day page**: the bound in force beside the value, as a fact, without a
  verdict.
- **Ledger** detail table: a Goal column at day grain.
- **Not**: Cross-metric, the Panel editor, anything pushed.

## Issues

1. `01-goal-history.md`: data, api: the Goal as a dated history
2. `02-attainment-on-the-series.md`: goal, query, api: Attainment on the Series
3. `03-web-set-a-goal.md`: web: setting a Goal on the Metric page
4. `04-web-the-line-and-the-counts.md`: web: the line and the counts on a Panel
5. `05-day-and-ledger.md`: day, web: the Goal on the Day and in the Ledger, and
   the docs pass

## Out of scope

- A Goal per week, or any declared grain.
- A `between` direction.
- Goals on `latest` Metrics, or on one State of a by-state Metric.
- Deriving a Goal from the Plan, or copying a Plan target into one.
- Colour, ticks, streaks, scores, notifications.

## Docs

- ADR 0044 and the CONTEXT.md entries land with this PRD.
- `05` carries the README and the ROADMAP row.
