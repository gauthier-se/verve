# PRD: The Day page

## Goal

A Panel shows a spike on 3 March. Clicking it does nothing, and nothing anywhere
in Verve answers "what happened on 3 March".

Every read in the app is a range and a bucket. The two screens that read one
bounded thing, a **Night** and a **Session**, are reached from their own lists
and know nothing of each other. So the day that carried a two-hour ride, four
hours of sleep, a note saying "flight to Tokyo" and a resting heart rate eight
beats above normal is stored completely, in four families, and displayed as four
unrelated points on four unrelated screens.

The date is the one coordinate every family already shares. A **Measurement**
has a day, a **Session** starts on one, an **Annotation** is dated, a Night is
named by the morning it woke on, an **Exclusion** covers a span of them. Verve
holds everything needed to answer the question and offers no page that asks it.

This milestone adds that page. It adds no new axis, no new grain and no new
family: it is composition, and the single decision it makes is what a Day is
allowed to be.

## The concept

**A Day is already a unit of decision, not a reader's convenience.** ADR 0034
elects a **Source** per Metric *per day*, because that is the grain at which the
evidence actually changes. The read engine therefore decides, for every metric
and every date the Account holds, which device won, and that decision is
currently invisible everywhere. A page for one date is not a new concept
arriving: it is the display of one the engine has been using since the second
connector landed.

**A Day is not an entity.** This is the whole constraint. A Night and a Session
have identity: a start, an end, a Source, a row that can be fetched. A Day has
none of that. It is a coordinate, and the moment it is treated like an entity it
acquires the one thing ADR 0041 refused it, an axis of its own: the day against
the clock, every metric on it, zoomable. That is the sub-day Series contract
wearing a date, and it is exactly what ADR 0012 and ADR 0041 exist to prevent.

**So the page is an index, not a chart.** What the Day does is name what has
identity on that date and link to it. The figures it carries are the very
buckets `/v1/series` already serves, at day grain, for one day, folded by each
Metric's own rule. Every intra-day *shape* stays behind a link to the entity that
owns it: the Night's stages at `/nights/$date`, a ride's heart rate at
`/workouts/$id`. The cap holds by construction rather than by discipline, because
the Day page has no axis to widen: there is no parameter to add, no control to
resist.

**One call, because one date has to mean one thing across six families.** A page
that assembled itself from six endpoints would decide client-side which Night
belongs to 3 March, which side of midnight a workout falls on, and whether a
missing row is a gap or a refusal. Those are read-path questions with one right
answer each. `GET /v1/plan` already set the precedent, answering a whole page in
one call so the client never re-computes; a Day read is a module in
`internal/query` for the same reason (ADR 0037).

**The Night on a Day is the night that woke into it.** The only genuine
ambiguity in the milestone. A Night is noon-anchored and labelled by the morning
it wakes into (ADR 0027), so a calendar day holds the tail of one night and the
head of the next. The Day for 3 March shows the Night labelled `2026-03-03`, the
night of the 2nd into the 3rd: it is the night that explains that day, and it is
the figure the sleep Panel already draws on 3 March. A page and a bar that
disagreed about the same night would be worse than no page, and sharing the
label is the only way to guarantee they cannot.

**A Workout belongs to the day it started on**, with none of the Night's shift,
exactly as ADR 0040 already decided for Training volume: a ride that runs past
midnight is one its owner names by the evening it began. The Day inherits that
rule rather than restating it.

**A gap and a refusal are different facts, and only the Day can say so in
place.** A Metric absent on a date because nothing was recorded, and one absent
because an **Exclusion** refuses it (ADR 0033), look identical on every screen
Verve has. On the Day the rule covering that date is right there, so the page
says "excluded" where every other screen can only draw nothing.

**Day**:
One calendar date read as a page: the index of everything the Account holds on
it, and the display of the per-day **Source** election ADR 0034 already
performs. A Day is a **Bucket** and not an entity: it has no identity, no row,
no start and end of its own, and therefore no **Trace**. Its figures are the day
buckets `/v1/series` serves, its **Night** is the one labelled by that date, its
**Sessions** are the ones that started on it (ADR 0040), and every shape finer
than the day sits behind a link to the **Night** or **Session** that owns it
(ADR 0041). An empty Day is an empty page and never a 404: a date exists whether
or not anything happened on it, which is the sharpest difference between a Day
and every other addressable thing in Verve.
_Avoid_: Daily summary (Summary is the Panel's headline figure, ADR 0019),
Journal / Diary (both imply authored entries; a Day is overwhelmingly a read),
Timeline (the History band's word), Day view (it names a Dashboard mode that
does not exist), Today (that is one Day, and the page is every Day).

## What this milestone does

- **`GET /v1/days/{date}`**, a `Day` read module in `internal/query` composing
  what already exists, never re-deriving it:
  - `metrics[]`, one row per Metric with data on that date, in Catalog order:
    slug, unit, aggregation, the day's folded `value`, and the `source` that won
    that day (ADR 0034). The set comes from `MetricsWithData` scoped to the day,
    and each fold goes through the Metric's own rule, so a Day figure and the
    bar above the same date are the same number by construction.
  - **The Account's Pins are hoisted to the top of that list, present or
    absent.** A pinned Metric with no data on a date is the most informative row
    on the page: it says the day is missing, not that the metric is. This does
    not give a Pin a time axis and does not reopen ADR 0025: the Day supplies
    the date, the Pin supplies only the ordering and the guarantee of a row.
  - `night`, the figures of the Night labelled by that date, read through
    `Engine.Night` rather than recomputed, and carrying no intervals: the shape
    is a link, not a payload.
  - `sessions[]`, the workouts that started on that date, in the summary shape
    `GET /v1/sessions` already returns.
  - `annotations[]`, the notes dated that day (ADR 0030).
  - `manual_entries[]`, the Manual rows on that date with their ids, so they are
    deletable in place (ADR 0022).
  - `exclusions[]`, the rules covering that date, which is what turns an absent
    row from a gap into a refusal (ADR 0033).
  - `phase`, the Plan Phase the date falls in, if any: the one piece of standing
    context that explains a day rather than describing it.
  - **An empty date is a 200 with empty collections.** Not a 404. A malformed
    date is the validator's 422, the same as everywhere else.
- **A web page at `/days/$date`**: the figures as a list, the Night, the
  workouts, the notes and the manual rows as sections, each row linking to the
  page that owns the thing it names. Previous and next day navigation, and a
  date picker reusing the existing control.
- **The bucket becomes a link.** A day bar or point on a Panel chart, a row in
  the Ledger detail table, and a day on the History band all navigate to the Day.
  **Only at day grain**: a week or month bucket has no Day, and clicking it does
  nothing rather than guessing one. The sleep Metric's bars keep going to the
  Night, which they already address.
- **Manual entry from the Day**: the dialog opens pre-dated to the page's date
  instead of today, which is the one gesture the current dialog makes awkward.

## What this milestone does NOT do

- **No intra-day axis on the Day.** No clock, no **Trace**, no hypnogram inline,
  no heart rate curve. ADR 0041 stands exactly as written: the shape of a night
  is on the Night, the shape of a ride is on the ride, and the Day links to both.
  This is the refusal the whole milestone is built around, and the page has no
  parameter through which it could be walked back.
- **No sub-day bucket on `/v1/series`**, and a one-day range is still not a
  loophole (ADR 0012).
- **No Day as a Metric, no Panel of a Day, no Pin of a Day.** A Day is
  addressable, which is not the same as being a Metric, for the same reason a
  Night is not one.
- **No score, no ring, no rating, and no colour saying the day went well.** A day
  page is precisely where every other health app puts one, which is why the
  refusal has to be written down here rather than assumed from the ROADMAP.
  Verve cannot know which direction is good for a metric, and a date does not
  change that.
- **No comparison on the Day.** No baseline window, no "against your average",
  no delta. A comparison is a property of a **Series** over a window (ADR 0015),
  and a Day has no window. If a day should ever read against a distribution of
  days, that is a Series question and it needs its own milestone.
- **No week page and no month page.** A Day is the only bucket that is also
  something a person lived; a week and a month exist as grains and nothing else.
- **No editing beyond what the Day already reaches.** Manual rows delete and
  Annotations edit because both already do, everywhere. Nothing here lets an
  imported value be retyped, a Source overridden, or a workout reassigned to
  another date.
- **No Meals.** The family is deferred (CONTEXT.md), and the Day is where it will
  land when it is not. Nothing in this payload is shaped to pre-empt it.
- **No stored aggregate.** Every figure derives from stored rows at request time,
  like every other read. A materialised day table is a second copy that can
  disagree with the first.

## Ordering

`01` is the read and stands alone, testable without the router the way ADR 0037
intends. `02` needs `01`. `03` is the set of links into the page, which can only
be written once there is a page to link to, and it carries the docs pass because
the README's claim is about navigation and not about either half separately.

## Docs

- **A new ADR** (`0043`): a Day is a bucket read as an index, not an entity. It
  opens on ADR 0034, which made the day a unit of decision before anything
  displayed it, records the three composition rules (the Night is the one
  labelled by the date, a Session belongs to the day it started, an empty date
  is a 200), and lists what was refused: an intra-day axis on the Day, a sub-day
  bucket, a Day as a Metric, a score, a comparison, and a week or month page.
- **ADR 0041** gains a pointer: the Day is the case that proves its rule rather
  than the exception to it, because it is addressable and still carries no axis.
- **ADR 0034** gains a pointer: the per-day Source election now has one place
  where it is visible.
- **A CONTEXT.md entry** for **Day**, beside **Night**, with the Avoid list
  above, and the **Night** entry gains a clause naming which Day shows it.
- **README.md**: a bullet under "Look at it properly", and the Sleep and Workouts
  bullets each gain a clause saying the Day reaches them.
- **ROADMAP.md**: a shipped row.

## Issues

1. `01-the-day-as-a-read`, query and api: the `Day` module composing metrics,
   Night, sessions, annotations, manual rows, exclusions and phase;
   `GET /v1/days/{date}`; the empty-date 200; ADR 0043 and the CONTEXT.md entry.
2. `02-web-the-day-page`, web: the `/days/$date` page, its sections, previous and
   next navigation, the date picker, and manual entry pre-dated to the page.
3. `03-web-the-bucket-is-a-link`, web: day-grain navigation from the Panel chart,
   the Ledger detail table and the History band, the no-op at week and month
   grain, plus the docs pass.
