# A `latest` Metric can carry a Goal, judged on the day's last reading

_Amends ADR 0044, which stays in force for everything else: a Goal is still a
declared daily bound, two directions, a dated history, counted and never graded._

## Context

ADR 0044 made every Metric eligible for a Goal except a `latest` one (body mass,
body fat, lean mass, VO2 max), on meaning rather than mechanics: "75 kg" was read
as a destination reached once, and "am I heading there?" as a Phase's question.

In use, the first Goal an owner reaches for is on the scale. Refusing it sends
them to the Plan, which answers a different question (a rate of change towards a
target, with energy behind it) and asks for far more to be declared. What they
wanted is the same thing a Goal already gives every other Metric: a line on the
Panel, and how many of the days they measured sat on the right side of it.

The mechanics never stood in the way. A `latest` Metric's day bucket is the last
reading of that day, the same value the Panel draws, so a day can be judged
against a bound exactly as a sum or an average is.

## Decision

**Every Metric is eligible for a Goal.** The `latest` exclusion is removed from
the API and from the Metric page; eligibility is no longer a rule at all.

**A `latest` day is judged on that day's last reading**, the value of its day
bucket, which is what the Panel draws at day grain. Nothing about Attainment
changes: covered, measured and met are counted as for any Metric.

**A day with no reading is a gap, never carried forward.** The last weigh-in is
not repeated onto the days after it. A Goal counts the days that were measured,
and a week without a scale is a week of gaps, not a week "under 75 kg". This is
the one place where `latest` could have tempted a special case, and it gets none.

**The Phase keeps its question.** A Goal on body mass says which measured days
sat under the bound; it does not say whether the owner is heading anywhere. The
Plan still owns trajectory, rate and energy.

## Considered Options

- **Allow `latest`, judge the day's last reading, no carry-forward (chosen).**
- **Keep the exclusion (ADR 0044 as written).** Principled, and it refuses the
  first Goal most owners try to set. Rejected.
- **Carry the last reading forward so every day is judged.** It would count days
  nobody weighed in as met or missed, which is inventing data. Rejected.
- **A target line without Attainment for `latest` Metrics.** A second, lesser
  kind of Goal with its own rules and its own UI, for no gain over counting the
  measured days. Rejected.

## Consequences

- `validateGoalMetric` accepts any Catalog Metric; `goalEligible` goes away and
  the Goal card shows on every Metric page.
- The CONTEXT.md entry for **Goal** loses its `latest` exclusion.
- Attainment on a `latest` Metric typically has a small `measured` beside a large
  `covered`, because most people do not weigh in daily. Showing the denominator,
  as ADR 0044 already requires, is what keeps that honest.
