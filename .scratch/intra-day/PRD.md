# PRD: Intra-day

## Goal

Two questions Verve holds the data for and cannot answer, and one line in the
README that says so:

> Sleep is read as durations per night, not as a hypnogram: the shape of a
> single night needs an intra-day axis Verve does not serve, and for the same
> reason a workout's detail view shows its recorded statistics and its trace but
> no heart rate curve.

The stages of a night are stored as intervals with their real timestamps. The
heart rate during a ride is stored as Measurements with theirs. Both are read
today only after being folded into a day, which is the right call for a
Dashboard and the wrong one for the two screens whose whole subject is the shape
of one bounded thing: was that night fragmented, and what happened to your pulse
on the climb.

ADR 0012 caps the read API at day resolution, deliberately, and this milestone
does not lift that cap. It notices that the cap was never the whole rule.

## The concept

**The intra-day axis already exists, once, and nobody minded.** A Route is
served as its own resource, not as a Series: a simplified polyline with its
elevation and pace profiles, derived from the stored artefact at read time, so
that "the day-resolution cap on the series contract is untouched" (ADR 0028, in
those words). A GPX trace is intra-day data on an entity's own page, and it
broke nothing, because it is not a Series, has no Dashboard range, and appears
on no Panel.

This milestone applies that same shape twice more.

**An axis belongs to an entity, never to a Panel.** The unit of an intra-day
read is one bounded thing with a start and an end: a **Night**, or a **Session**.
It is addressed by that thing's own identity, takes no range parameter, and is
drawn on that thing's page. There is no route by which a Dashboard, a Panel, a
Ledger row or the CSV can ask for an hour. `/v1/series` is untouched, and ADR
0012 stands as written.

**The grain is the entity's, not the caller's.** A Route is simplified to a
point cap the server chooses; an intra-workout series is bucketed to a fixed
number of buckets across the workout's own duration, so a 20-minute run and a
6-hour ride both come back bounded and neither has a grain a caller picked. That
is the property that keeps this from becoming a general sub-day API by
increments: there is no parameter to widen.

**A Night's shape is resolved exactly as its figure is.** The hypnogram is drawn
from the same rows, through the same resolution the `sleep` Metric applies: the
in-bed fallback, and the Source elected per Night (ADR 0027). A page and a bar
that disagreed about the same night would be worse than no page, and the only
way to guarantee they cannot is to share the resolution rather than to re-derive
it.

**The figures a night carries are properties of that night**, not Metrics.
Onset, wake, efficiency and time awake are what an intra-day axis makes
computable, and none of them survives folding: a mean onset over a month is a
clock time averaged across dates, and an efficiency over a week is a ratio of
sums pretending to be a rate. They are served on the Night, and this milestone
deliberately makes none of them a Catalog Metric.

**Night** (extension): the term already exists for the noon-anchored day a sleep
interval belongs to. It becomes addressable: `/v1/nights/2026-03-02` is the
night that woke on that morning, and the page is the shape of it.

**Trace**:
The shape of one bounded entity read against the clock: a **Night**'s stages, a
**Session**'s heart rate, a **Route**'s elevation. Always a resource on the
entity, never a **Series**, never on a **Panel**, and always bounded by the
entity's own start and end. Its grain is derived from that span by the server.
_Avoid_: Intra-day series (it invites "then let me pick the bucket"), Detail
series, Raw data (a Trace is simplified or bucketed, never raw), Hypnogram (that
is one Trace's rendering, for one family), Timeline (the History band's word).

## What this milestone does

- **`GET /v1/nights/{date}`**, the Night as a resource: its intervals
  (`state`, `start_at`, `end_at`) in order, the Source that won it, and the
  figures below. `date` is the Night label, the morning it woke on, which is what
  every sleep Point is already keyed by. An unknown or empty night is a 404.
- **The figures**, computed in `internal/query` beside the Metric they belong
  with, each nil rather than zero when the evidence is absent:
  - `asleep` minutes, the same number the Metric's Point carries for that night;
  - `onset` and `wake`, the first asleep start and the last asleep end, as
    timestamps rather than durations;
  - `awake` minutes between onset and wake, which is what a fragmented night has
    and a deep one does not;
  - `efficiency`, asleep ÷ (wake − onset), as a percentage, which is the
    definition available when `in_bed` is absent, and the payload names the
    denominator it used rather than leaving the reader to assume the classic
    time-in-bed one;
  - `latency`, only when an `in_bed` interval precedes the first asleep one. A
    Watch-only night has no latency and says so with an absent field.
- **`GET /v1/sessions/{id}/series?metric=heart_rate`**, the workout's own curve:
  the Measurements whose interval falls inside the Session's window, bucketed
  into at most 300 buckets across the workout's duration, each carrying its mean
  and the number of readings behind it. A workout with no samples of that Metric
  is an empty series and not a 404: the workout exists, the curve does not.
- **The Metric is validated against the Catalog** and must be `average` or `sum`
  by rule. A `latest` Metric has no meaning inside an hour, and a by-state one is
  not in `measurements` at all.
- **The Source is resolved per workout, not per day.** Inside one session the
  question "which device" has one answer for the whole span, so the read elects
  once over the window using the same priority table (ADR 0003), and reports it.
- **The web draws the hypnogram** on a new page at `/nights/$date`: the stages as
  bands against the clock from onset to wake, the figures above it, and the
  Source named. It is reached from the sleep Metric page, whose chart bars and
  table rows both address a Night already.
- **The web draws the workout curve** on the Session page, under the map, with
  the same axis the elevation profile uses so the two read as one picture of the
  same ride. Absent data is an absent chart, not an empty one.
- **The README's paragraph is rewritten**, since it is the one place the old
  limitation is promised.

## What this milestone does NOT do

- **No sub-day bucket on `/v1/series`.** Not as a parameter, not as a preset,
  not for a single-day range. ADR 0012 stands: a Dashboard reads days, and every
  screen that reads finer is looking at one entity.
- **No Panel of a night, and no Pin.** A Night is now addressable, which is not
  the same as being a Metric. It has no bucket, no window and no comparison.
- **No sleep figure as a Catalog Metric.** Onset, efficiency and the rest are
  per-night properties that do not fold, and a `sleep_efficiency` Metric would
  have to answer "what is the efficiency of a month", which is a ratio of sums
  wearing a rate's unit. If one is wanted later it needs its own rule, its own
  ADR and a defensible fold, none of which this milestone guesses.
- **No stored downsample.** Both reads derive from stored rows at request time,
  the way a Route's polyline already does (ADR 0028). Caching a curve is a
  second copy that can disagree with the first.
- **No intra-day Annotation, and no intra-day Exclusion.** Both are day-granular
  because every bound Verve stores is (ADR 0030), and nothing here changes what
  is stored.
- **No editing a night.** The hypnogram is a read. Splitting, merging or
  retyping a stage is data entry for a family the Manual entry path deliberately
  refuses.
- **No second Metric on a workout curve at once.** One Metric, one axis: two
  units inside a ride is the dual-axis question a Panel answers, and a workout
  page is not a Panel. It is a parameter, so asking twice is two requests.
- **No zoom, pan or brush.** The window is the entity's own span, fixed. A
  control that changes it is the caller-chosen grain this milestone exists to
  avoid.

## Ordering

`01` and `02` are independent: one reads States for a Night, the other
Measurements inside a Session, and neither shares code with the other beyond the
ADR that admits them. `03` needs `01`, `04` needs `02`. The docs pass rides with
`04` because the README's paragraph names both halves in one sentence.

## Docs

- **A new ADR** (`0041`): an intra-day axis exists inside an entity and never on
  a Panel. It opens on the Route, which has been doing exactly this since ADR
  0028, records that the grain is derived from the entity's span rather than
  chosen by a caller, and lists what was refused: a sub-day bucket on
  `/v1/series`, a single-day range as a loophole, a Night as a Metric, the sleep
  figures as Catalog Metrics, a stored downsample, and zoom.
- **ADR 0012** gains a pointer: the cap it sets is on the Series contract, and it
  was never a statement that Verve holds no intra-day data. ADR 0027's "the shape
  of a single night needs an axis the API does not serve" gains the same.
- **A CONTEXT.md entry** for **Trace**, beside **Route**, and the **Night** entry
  gains its new half: it is now addressable as a resource.
- **README.md**: the "does not do" paragraph is rewritten, and the Sleep and
  Workouts bullets each gain a clause.
- **ROADMAP.md**: a shipped row, and three Later entries go: the hypnogram,
  intra-workout series, and the questions around a night.

## Issues

1. `01-the-night-as-a-resource`, query and api: the Night read reusing the
   Metric's own resolution, the figures and their absences, `GET /v1/nights/{date}`,
   ADR 0041 and the CONTEXT.md entries.
2. `02-the-workout-curve`, query and api: the bucketed read inside a Session's
   window, the per-workout Source election, the bucket cap,
   `GET /v1/sessions/{id}/series`.
3. `03-web-the-hypnogram`, web: the `/nights/$date` page, the stage bands against
   the clock, the figures, and the links from the sleep Metric page.
4. `04-web-the-workout-curve`, web: the curve under the map on the Session page,
   sharing the profile's axis, plus the docs pass.
