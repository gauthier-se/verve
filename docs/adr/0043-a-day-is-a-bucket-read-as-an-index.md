# A Day is a bucket read as an index, not an entity

## Context

Every read in Verve is a range and a bucket. The two screens that read one
bounded thing, a Night and a Session, are reached from their own lists and know
nothing of each other. So a Panel could show a spike on 3 March and nothing in
the app answered what happened on 3 March: the ride, the four hours of sleep,
the note saying "flight to Tokyo" and the resting heart rate eight beats above
normal were stored completely, in four families, and displayed as four unrelated
points on four unrelated screens. Clicking the bucket did nothing, which made the
most common gesture in the app inert.

The date was already the one coordinate every family shares. A Measurement has a
day, a Session starts on one, an Annotation is dated, a Night is named by the
morning it woke into, an Exclusion covers a span of them.

More than that, the day was already a unit of decision. ADR 0034 elects a Source
per Metric *per day*, because that is the grain at which the evidence actually
changes. The read engine has been deciding, for every metric and every date an
Account holds, which device won, and nothing displayed that decision anywhere.

The risk was equally clear. A page for one date is one step from an axis for one
date: the day against the clock, every metric on it, zoomable. That is the
sub-day Series contract wearing a date, and it is what ADR 0012 and ADR 0041
exist to prevent.

## Decision

**A Day is a Bucket read as a page, and it is not an entity.** A Night and a
Session have identity: a start, an end, a Source, a row that can be fetched. A
Day has none of that. It is a coordinate, so it gets an index of what has
identity on it, and never a shape of its own.

**The page carries figures and references, never a Trace.** Its figures are the
day buckets `/v1/series` already serves, folded by each Metric's own rule. Every
intra-day shape stays behind a link to the entity that owns it: the Night's
stages at `/nights/{date}`, a ride's heart rate at its Session. ADR 0041 stands
exactly as written, and the cap holds by construction rather than by discipline,
because a Day read has no axis and therefore no parameter to widen.

**One call, because one date has to mean one thing across six families.**
`GET /v1/days/{date}` answers the whole page. Which Night belongs to this date,
which side of midnight a workout falls on, whether an absent row is a gap or a
refusal: those are read-path decisions with one right answer each, and assembled
client-side they would live in a component and drift from the engine that made
them. The Plan set the precedent (ADR 0023), deriving its targets server-side so
the client never re-computes.

**The Night on a Day is the one that woke into it.** A Night is noon-anchored and
labelled by the morning it wakes into (ADR 0027), so a calendar day holds the
tail of one night and the head of the next. The Day for 3 March carries the Night
labelled `2026-03-03`, which is the figure the sleep Panel already draws on that
date. Sharing the label is the only way to guarantee the page and the bar cannot
disagree, and the Day goes through the entity read rather than re-deriving
anything.

**A Session belongs to the day it started on**, with none of the Night's shift,
exactly as ADR 0040 already decided for Training volume: a ride that runs past
midnight is one its owner names by the evening it began. The Day inherits that
rule rather than restating it.

**An empty date is a 200 with empty collections, never a 404.** A date exists
whether or not anything happened on it, which is the sharpest difference between
a Day and every other addressable thing in Verve. A malformed date is a 422: the
request is wrong, not the data.

**A gap and a refusal are different facts, and the Day is where they are told
apart.** A Metric absent because nothing was recorded and one absent because an
Exclusion refuses it (ADR 0033) look identical on every other screen. On the Day
the rule covering that date is present, so the row says excluded rather than
drawing nothing.

**Pins order the list and gain nothing else.** The Account's Pins appear first,
present or absent, and a pinned Metric with no data that day is the most
informative row on the page: it says the day is missing, not that the Metric is.
This does not reopen ADR 0025: the Day supplies the date, the Pin supplies only
the ordering and the guarantee of a row.

## Why

- **The decision was already being made.** The Source election runs per day
  (ADR 0034). This ADR adds no rule, it surfaces one, and that is the difference
  between a Day page and a dashboard of convenience.
- **An index cannot drift into an axis.** There is no control to resist later,
  because there is no window on the page to widen and no grain on the payload to
  refine. The same property that made `/v1/nights/{date}` safe (ADR 0041).
- **One date has to mean one thing.** Six endpoints assembled by a component
  would make the client decide which night belongs to a date. It would be right
  on the first afternoon and wrong the first time the Night rule moved.
- **A read is a module.** The composition is a module of its own for the reason
  `internal/history` is: its parts belong to the read engine and to five tables
  the read engine has no business naming, and an HTTP handler is where that kind
  of composition goes to rot (ADR 0037).

## Considered Options

- **An intra-day axis on the Day** (every metric against the clock). Rejected.
  It is the sub-day Series contract with a date for a parameter, and it would
  make the Day the loophole ADR 0041 closed. The shape of a night is on the
  Night; the shape of a ride is on the ride.
- **A Day as an entity, with a row and a resolved identity.** Rejected: nothing
  would be in that row. A Day has no start, no end and no Source of its own, and
  materialising one invites the axis above.
- **A Day as a Metric, with a Panel and a Pin.** Rejected for the reason a Night
  is not one: a date is not a quantity, and it has no bucket, no window and no
  comparison.
- **A score, a ring, or a colour saying the day went well.** Rejected, and named
  here rather than left to the ROADMAP because a day page is precisely where
  every other health app puts one. Verve cannot know which direction is good for
  a metric, and a date does not change that.
- **A comparison on the Day** ("against your average"). Rejected for this
  decision: a comparison is a property of a Series over a window (ADR 0015) and a
  Day has no window. Reading a day against a distribution of days is a Series
  question and needs its own ADR.
- **A week page and a month page**, for symmetry. Rejected: a Day is the only
  bucket that is also something a person lived. A week and a month are grains.
- **Assembling the page from the six existing endpoints.** Rejected: see above.
  It is also six round trips for one screen.
- **Materialising a per-day table.** Rejected: a second copy that can disagree
  with the first, which is what every read in Verve already refuses.

## Consequences

- `query.NightDetail` is split: `NightSummary` carries the figures and
  `NightDetail` embeds it and adds the intervals. The wire shape of
  `/v1/nights/{date}` is unchanged but for field order, and there is now one
  definition of what a night's efficiency is rather than two.
- `MetricsWithData` gains a window (`MetricsWithDataIn`) rather than a day-scoped
  twin. That function exists so a caller never has to know sleep is States and
  training is Sessions, and a copy would have forgotten both on the day a third
  family arrives.
- The Day is Account-scoped like every other read, so another Account's date is
  empty rather than forbidden.
- A day's workouts are capped (200) so a pathological import cannot make one page
  unbounded. A day past the cap is not a day.
- The Day is the natural home for a Manual entry, because it is the one screen
  that already knows which date you mean.
- The bucket becomes a destination, which is a change to how the app is
  navigated: every day-grain bar, point and dated row now has somewhere to go.
  A week or month bucket has none, and must do nothing rather than guess one.
