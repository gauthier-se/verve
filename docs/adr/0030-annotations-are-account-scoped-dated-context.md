# An Annotation is Account-scoped dated context, folded onto the bucket grid

## Context

Verve shows what the data says and refuses to interpret it: it never colours a
change good or bad, because it cannot know which direction is good for a given
Metric (ADR 0011). The owner does know, but nothing they know is in the data. A
week of flat steps was a week in bed; a mass plateau began when the program
changed; ten days of elevated resting heart rate were a trip with bad sleep.
None of that arrives from Apple Health, and until now there was nowhere in Verve
to put it, so every reading of a chart repeated the same act of memory.

An Annotation is that memory, dated and on the time axis. It is the fourth
differentiator promised alongside period comparison (ADR 0015), cross-metric
Panels (ADR 0020) and derived Metrics (ADR 0014), and the last one unbuilt.

Three questions had to be answered before any of it could be written down: what
an Annotation belongs to, whether it is a kind of Measurement, and where a
marker is placed on a chart whose X axis is a list of categories the server
emitted.

## Decision

**An Annotation belongs to the Account; showing it belongs to the Dashboard.**
The note "flu, 12-19 March" is a fact about the year, not about the Training
dashboard: it is written once and appears wherever that fortnight is on screen,
read against steps, sleep and mass without being entered three times. Whether a
given Dashboard draws its markers is a `dashboards.annotations` boolean, stored
beside `baseline_rule` (ADR 0015), because the Dashboard owns the time axis and
the Panels own the metric axis. It defaults to on, including for every Dashboard
that predates the migration: a feature nobody can see is a feature nobody
enables.

**An Annotation is not a Measurement.** It carries no value, no unit, no Metric
and no aggregation rule, it has its own table and its own endpoints, and it never
touches `/v1/series` or the Catalog. The Metric it is read against is whichever
Panel happens to be on screen, which is exactly the property a Measurement does
not have.

**An Annotation is a day or a span of days**, `starts_on` plus an optional
`ends_on`, both `YYYY-MM-DD` and no clock, because the whole read path stops at
the day (ADR 0012). A single day renders as a marker and a span as a band. There
is no third shape.

**The server folds an Annotation onto the bucket grid.** `GET /v1/annotations`
takes the same time-axis tokens `/v1/series` takes and returns each note carrying
both its real dates and its grid position: `bucket`, and `end_bucket` only when
the span covers more than one bucket. The arithmetic lives in `timeaxis`
(`Resolved.Fold`), over `query.Bucket.Start`, so one module still owns bucket
boundaries. With no tokens at all the same endpoint returns the Account's whole
history, unfolded: no axis, no buckets.

**Every Annotation is deletable**, unlike a Measurement, which is deletable only
when its Source is Manual (ADR 0022). They are all typed by their owner, so the
distinction that rule protects does not exist here.

## Considered Options

- **Account-scoped, Dashboard-shown (chosen).** One note, every chart, one
  switch per Dashboard for the chart where it is noise.
- **Per-Dashboard Annotations.** Cheaper to reason about, and wrong the first
  time someone wants their illness visible on both the Training and the Sleep
  dashboard. Re-entering the same fortnight twice is how a feature stops being
  used. Rejected.
- **An Annotation as a Catalog entry**: a `label` family with no unit, carried
  on `/v1/series` beside the Metrics. It would inherit the range, the buckets and
  the Panels for free, and it would poison the Catalog: every consumer of a
  Series would have to ask whether this one has a value. Rejected.
- **Client-side bucket resolution.** The client holds the grid and could snap a
  date to it. That is a second, untested implementation of "which bucket holds
  this day", in a language where nothing pins it against the Go and SQL pair the
  Time axis milestone deliberately pinned together. Its failure mode is the worst
  available: a disagreement of one boundary rule renders nothing at all, with no
  error anywhere. Rejected.
- **A closed set of kinds** (illness, travel, program, injury…), with a colour
  each. A taxonomy of someone's life is not Verve's to define, and a free label
  says "flu" better than a colour does. Deferrable in one direction only, so
  deferred. Rejected for now.
- **Sub-day Annotations.** "The run at 18:30" needs an intra-day axis the API
  does not serve (ADR 0012). It arrives with the hypnogram or not at all.
  Rejected.
- **Metric-scoped Annotations.** A note attached to `resting_heart_rate` would be
  invisible on the Panel where its effect actually shows. If a note is about one
  Metric, its label says so. Rejected.
- **Derived Annotations** from imports, Phases or workouts. ADR 0028 names the
  temptation to draw a Session as a marker; an import boundary and a Phase
  boundary are the same temptation. Everything here is typed by the owner.
  Rejected.

## Consequences

- The list endpoint filters by **overlap**, not by start day:
  `starts_on < to AND COALESCE(ends_on, starts_on) >= from`. A span that began
  before the range and is still running is on screen for the days it covers, and
  a filter on `starts_on` alone would drop exactly the case that matters. A span
  overhanging either end is clamped to the window's own first and last bucket.
- `end_bucket` is emitted **only when it differs from** `bucket`, so its presence
  is the client's whole "band, not marker" test. A fortnight at the month grain
  is one bucket wide and has no band to draw.
- **Markers describe the current range only.** No Baseline token is read: a
  Baseline series is laid on the current window's ordinal axis (ADR 0015), so a
  marker over it would sit at a bucket whose date is not the date under it.
- A Series is **sparse**: its points come from a `GROUP BY`, so a bucket with no
  data is absent from the payload and therefore absent from the chart's category
  axis. A marker whose bucket is one of those is not drawable at its exact place,
  and the case is not hypothetical: the illness that flattens a curve is often
  the week that empties it. The client places such a marker on the first drawn
  category at or after the folded bucket (a lexical comparison over
  server-produced `YYYY-MM-DD` strings, no date arithmetic), and the tooltip
  always names the note's real dates, so nothing on screen lies about when it
  happened. Making Series dense is the real fix and is a change to the read path,
  not to this feature.
- An empty string clears a body or an end day on `PATCH`; JSON `null` does not,
  because it decodes to the same nil pointer as an absent field. That is the
  existing convention of this API, not a new one.
- If Annotations ever need kinds, colours, or a Metric of their own, this is the
  ADR to reopen, and the question to answer first is what the Account gains that
  a longer label does not already give.
