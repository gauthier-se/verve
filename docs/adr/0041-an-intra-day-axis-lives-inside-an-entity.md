# An intra-day axis lives inside an entity, never on a Panel

## Context

ADR 0012 caps the read API at day resolution. It was the right call and it is
still the right call for a Dashboard: a Panel reads buckets, the finest bucket is
a day, and every screen built on `/v1/series` inherits that.

It was never a statement that Verve holds no intra-day data. Sleep stages are
stored as intervals with their real timestamps. Heart rate during a ride is
stored as Measurements with theirs. Both are read only after being folded, which
loses exactly the thing two screens are about: whether that night was
fragmented, and what happened to your pulse on the climb. The README promised
that limitation in as many words, and the roadmap carried three entries waiting
on it.

The cap also already has an exception nobody minded. A Route is served as its
own resource rather than as a Series, a simplified polyline with its elevation
and pace profiles derived from the stored artefact at read time, and ADR 0028
recorded that this leaves "the day-resolution cap on the series contract
untouched". A GPX trace is intra-day data, on an entity's own page, and it broke
nothing.

## Decision

**An intra-day axis belongs to an entity, never to a Panel.** The unit of such a
read is one bounded thing with a start and an end: a **Night**, or a **Session**.
It is addressed by that thing's identity, takes no range parameter, and is drawn
on that thing's page.

**`/v1/series` is untouched.** No sub-day bucket, no single-day special case, no
parameter. ADR 0012 stands as written, and this ADR narrows what it was ever
claiming: the cap is on the Series contract, not on the data.

**The grain is the entity's, not the caller's.** A Route is simplified to a point
cap the server picks; a workout's curve is folded into a fixed number of buckets
across the workout's own duration. A 20-minute run and a 6-hour ride both come
back bounded, and there is no parameter to widen. That is the property that keeps
this from becoming a general sub-day API by increments.

**A Night's shape is resolved exactly as its figure is.** The hypnogram runs the
sleep Metric's rows through the sleep Metric's own resolution: the in-bed
fallback, and the Source elected per Night (ADR 0027). One implementation, two
readers. A page and a bar that disagreed about the same night would be worse
than no page.

**The figures an axis makes computable stay on the entity.** Onset, wake, time
awake between them and efficiency are properties of one night, and none of them
folds: a mean onset over a month averages clock times across dates, and an
efficiency over a week is a ratio of sums wearing a rate's unit. They are served
on the Night and are deliberately not Catalog Metrics.

**A figure whose denominator depends on the evidence names it.** Efficiency is
reported with the span it was computed over, because the classic definition
(time in bed) is unavailable exactly when the richer evidence exists: the
resolution that makes the page agree with the chart drops in-bed rows whenever
real Stages are present. The same move the Plan makes with its expenditure basis
(ADR 0023).

## Why

- **The boundary is structural, not disciplinary.** `/v1/nights/{date}` cannot be
  pointed at a range, a Dashboard or a Panel, because it takes no range at all.
  A `day=` parameter on `/v1/series` would have been the same feature with the
  cap enforced by nothing but review.
- **The precedent had already run.** The Route has been doing this since the
  workouts milestone, and the properties that made it safe (entity-scoped, server
  chosen grain, derived at read time, no Panel) are the four this ADR generalizes.
- **Folding is what these questions are not.** Every figure here exists because
  there is an axis. Putting them in the Catalog would force an answer to "what is
  the efficiency of a month", and the honest answer is that there is none.
- **One resolution, two readers.** Sleep's in-bed rule and per-Night Source
  election are subtle enough that a second implementation would drift, and the
  drift would show up as a page contradicting the chart directly above it.

## Considered Options

- **A sub-day bucket on `/v1/series`** (`bucket=hour`, or a `day=` shortcut).
  Rejected: it hands every existing caller a finer grain, which is ADR 0012
  reversed in practice while claiming to be upheld. The Dashboard, the Ledger,
  the CSV and the Panel summary would each have to answer what an hour means.
- **A Night as a Metric with an intra-day axis.** Rejected: a Night is not a
  quantity, and making it one requires a bucket, a window and a comparison for
  something that has none of the three.
- **Sleep onset, efficiency and friends as Catalog Metrics.** Rejected for now,
  and the reason is a fold, not effort: they have no defensible aggregation. One
  of them may earn a rule later, and it will need its own ADR.
- **Latency as a fifth figure.** Dropped during implementation rather than
  deferred, because the resolution makes it uncomputable: an in-bed interval and
  a staged interval never survive together in the kept set, so a latency field
  would have been absent on every night that has an onset at all. A field that
  can never be populated is worse than an absent feature.
- **Storing a downsampled copy.** Rejected: a second copy that can disagree with
  the first. The Route already derives at read time and that has held.
- **Zoom, pan or a brush on the intra-day chart.** Rejected: the window is the
  entity's own span, and a control that changes it is the caller-chosen grain
  this ADR exists to prevent.
- **Intra-day Annotations.** Not reopened: every bound Verve stores is
  day-granular (ADR 0030), and serving a finer axis does not change what is
  stored.

## Consequences

- `resolveNights` is refactored into a per-Night `resolveNight` that also returns
  the rows it folded, which is what lets the entity read share the rule rather
  than copy it. It is the one change this ADR forces on existing code.
- `/v1/nights/{date}` answers 404 for a night nothing recorded, which is a fact
  about the data rather than an error, and 422 for a date that is not one.
- A Night is now addressable, which is not the same as being a Metric: no Panel,
  no Pin, no Ledger row, no bucket.
- The README's "what Verve does not do" paragraph loses the hypnogram sentence.
  What replaces it is narrower and still true: a Dashboard reads days, and an
  intra-day axis exists only inside an entity.
- `efficiency_basis` today has one value. It is a field rather than a constant so
  that the day a time-in-bed basis becomes available, the number's meaning
  changes in the payload rather than silently on screen.
