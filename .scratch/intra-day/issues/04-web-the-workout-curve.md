Status: done
Blocked by: 02

# 04: web: the curve under the map, and the paragraph that is no longer true

## What

- **The Session page gains the curve** (`session-detail.tsx`), under the map and
  above or beside the elevation and pace profiles it already draws. Same chart
  library, same axis treatment, so the ride reads as one picture: where you
  climbed, how fast you went, what your pulse did.
- **The Metric is heart rate by default**, and the control to change it lists
  only the Metrics the workout actually has readings for. Enumerating the
  Catalog and offering 108 options, 107 of which answer "no data", is the
  scoreboard's empty-row problem in a dropdown (ADR 0021). The simplest honest
  source for that list is the Session's own stats, which already name the
  Metrics the device reported for this workout.
- **No curve rather than an empty one.** A workout with no readings of the
  chosen Metric shows nothing where the chart would be, with one line saying so.
  The absence is the answer: a strength session from a phone has no heart rate,
  and an empty axis suggests a failure that did not happen.
- **The x axis is the workout's own span**, echoed by the payload, so the chart
  never has to guess it. The profiles are drawn against distance and the curve
  against time, which is a real difference and worth one word of labelling
  rather than a forced shared axis.
- **`lib/workout-series.ts`** for the pure part: payload points to chart data,
  the axis ticks, and the formatter picked by unit (the rule the breakdown work
  already established). Tested where the runner reaches.

## Docs, riding with this issue

- **README.md**: the "What Verve does not do" paragraph names both halves of
  this milestone in one sentence and is rewritten. What replaces it is narrower
  and still true: Verve serves an intra-day axis inside an entity, and a
  Dashboard still reads days. The Sleep and Workouts bullets each gain a clause.
- **ROADMAP.md**: a shipped row, and three Later entries leave: the hypnogram
  and the questions around a night, and intra-workout series.
- **ADR 0012 and ADR 0027** gain their pointers to ADR 0041 (see issue 01).

## Why here

The docs ride with this issue rather than with the first, because the sentence
that has to change names the night and the workout together. Changing it while
half of it is still true would be worse than leaving it.

The Metric picker is scoped to the workout's own stats rather than to the
Catalog because that is the list that answers the question being asked. It also
happens to be free: the Session detail payload already carries them.

## Comments

Shipped. `web/src/lib/workout-series.ts` with its test,
`hooks/use-workout-series.ts`, the `CurveSection` on the Session page, and the
docs pass across README.md and ROADMAP.md.

Two notes:

- **The picker comes from the workout's own stats**, as specified, and it turns
  out to be exactly right for a second reason: the stats are the Metrics this
  device reported *for this workout*, so a strength session offers energy and a
  ride offers heart rate, with no request made to find out.
- **The axis is elapsed time, labelled "12:30 in"**, while the route profiles
  stay on distance. Forcing one shared axis would have meant resampling one of
  them against the other, which invents points; two axes and one word of
  labelling is the honest version.

Smoke-tested on the real binary: `/v1/nights/2024-01-02` answers the imported
night with its onset, wake and efficiency, a night nothing recorded is a 404, a
malformed date is a 422, a workout with no heart rate is a 200 with no points,
and `body_mass` inside a workout is refused with the sentence that names why.

One payload detail worth recording: a staged night with no interruption reports
`awake: 0` rather than omitting it. That is a fact and not a fabricated zero,
because the resolved stages tile the night: the absence of an awake interval
between onset and waking is the evidence that there was none. An in-bed-only
night omits the field entirely, which is the case where there is no evidence.
