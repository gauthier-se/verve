Status: done
Blocked by: 01

# 02: web: the page for one date

## What

- **A page at `/days/$date`** (`web/src/components/day-page.tsx`, registered in
  `router.tsx` beside `/nights/$date`, which it is modelled on), fed by
  `hooks/use-day.ts`. One date, one call, five sections, and nothing on it that
  the API did not decide.

- **The figures, as a list and not as a chart.** One row per Metric: its label
  and icon from the Catalog the client already holds, the day's value formatted
  by `lib/format.ts`, and the Source that won the date set quietly beside it.
  The row links to the Metric page, the way a Ledger row does. A Day is an
  index: there is no chart on this page, because the only chart a single date
  could carry is the intra-day axis ADR 0041 puts on entities.

- **Pinned Metrics first, present or absent.** A pinned row with no value that
  day renders as an explicit absence in the app's own register, not as a dash
  that could be read as a zero. That row is the point: it says the day is
  missing, not that the metric is. An excluded Metric says "excluded" and links
  to the rule, which is the one place in the interface where a refusal and a gap
  are told apart (ADR 0033).

- **The Night section**: asleep, awake, onset and wake as clock times, and
  efficiency with its basis in words, reusing the formatters `night-page.tsx`
  already has rather than writing second ones. The whole block links to
  `/nights/$date` for the shape. No hypnogram here, not even a small one.

- **The Workouts section**: the day's Sessions in the row shape the list already
  renders, each linking to `/workouts/$id`. No map, no curve, no totals bar: the
  workout's own page owns all three.

- **Notes and manual entries**: the Annotations dated that day, editable in
  place through the existing dialog, and the Manual rows with their delete
  control, which works here for the same reason it works on the Data page
  (ADR 0022). Adding a manual entry from this page opens the dialog **pre-dated
  to the page's date** rather than to today, which is the one gesture the
  current dialog makes awkward and the main reason to write on a Day at all.

- **The Phase**, when the date falls in one, as a single line of standing
  context above the sections: it explains the day rather than describing it.

- **Previous and next day**, as controls and as left/right keys
  (`react-hotkeys-hook` is already a dependency), plus the existing date picker.
  Navigation is unbounded: a date before the Account's first row is a valid,
  empty Day, and the page says so.

- **The empty state is a real state, not an error.** A date with nothing on it
  renders its header, its navigation and one sentence. It is reachable by
  construction, so it is designed rather than discovered.

- **`lib/day.ts`** holds what is pure and therefore testable: ordering the
  metric rows (pins first, then Catalog order), deciding whether a row reads as
  a gap or as a refusal, and the previous/next date arithmetic. Its own
  `day.test.ts`, since that is the layer the runner covers.

## Why here

**A page rather than a drawer**, for the reason the Night got one: the first
thing anyone does with a strange day is send it to somebody, and a drawer is not
a link. `/nights/$date` is the precedent down to the header and the figure
block, and reusing its formatters is what keeps the same night from reading two
ways on two screens.

**No chart on the Day** is the decision this page exists to hold. Every instinct
says put the day's metrics against the clock, and that is exactly the sub-day
axis ADR 0041 confined to entities. The Day has no axis, so there is no control
to resist later: the refusal is structural rather than a matter of taste.

**Pins order the list and gain nothing else.** ADR 0025 refuses a Pin a time
axis, and nothing here gives it one: the Day supplies the date, the Pin supplies
only the ordering and the guarantee of a row. If that ever starts to look like a
range, this is the paragraph to reopen.

**No score and no colour.** A day page is where every other health app puts a
ring, and Verve does not know which direction is good for a metric. The refusal
is worth naming in the component that would carry it.

## Comments

Shipped. `web/src/lib/day.ts` with `day.test.ts` (14 tests), `hooks/use-day.ts`,
`components/day-page.tsx`, the `/days/$date` route, the `date` prop on
`ManualEntryDialog`, and the Day added to the invalidation of the three writes
that land on a date (manual entries, notes, exclusions).

It was run against a real import (1.6 M measurements, 269 workouts) rather than
only typechecked, which is where four of the five findings below came from.

- **The endpoint was unusable on real data, and `02` is where that showed.**
  2.65 s for one date, so the page spun and arrow-key navigation was pointless.
  Two unindexed reads, both introduced in `01`: `ListManualOn` filtered by source
  and date with no index leading on either (2.46 s), and `MetricsWithDataIn`
  filtered by date with no Metric named, which `measurements_account_metric_start`
  cannot seek (150 ms). Migration `0015` adds a partial index on the Manual rows
  alone, which fixes the Data page's entry list at the same time, and
  `MetricsWithDataIn` now probes the compiled Catalog per Metric in its windowed
  form and keeps the DISTINCT scan for the unbounded one the Ledger asks for.
  42 ms, 63× faster, no index over the 1.6 M imported rows.
- **A refused Metric had no row unless it happened to be pinned.** The payload
  carried the covering Exclusions and nothing rendered them, so the one thing
  ADR 0043 claims only a Day can say was invisible in exactly the common case. The
  row set is now pins ∪ metrics-with-data ∪ metrics-a-rule-covers.
- **A Manual value named the wrong Source.** `sourceFilter.reported()` keeps the
  imported Source when the overlay is active, deliberately and correctly over a
  window: the overlay is one day of many. On a Day the window *is* that day, so a
  typed body mass was being credited to the scale that did not produce it. The Day
  names Manual for a Metric it holds a typed row for, using rows it already reads.
- **Arrow navigation dropped presses.** The callback closed over the date of its
  render, so two presses inside one render both shifted from the same date. It now
  shifts from the params the router holds, which is what makes a held key advance
  a day per press.
- **`TestAnnotationWritesInvalidateOnlyTheNotes` failed, and it was right to.** It
  pins that a note refetches no aggregate. The Day is not an aggregate (one call,
  one date) and it displays the notes, so the test was widened to name the two
  queries a note may invalidate and still forbids the four it may not.

Three deviations from the spec, each smaller:

- **The client does not order the metric rows.** The spec put pins-first ordering
  in `lib/day.ts`; `01` had already put it in the read, where it belongs, because
  the server is what knows the Catalog order. `lib/day.ts` holds what was left:
  the date arithmetic, the gap-or-refusal reading, and the empty-day test.
- **Notes and entries have no empty sections.** The spec implied a section each
  with an add affordance. Two empty cards on a blank day is noise, so the two write
  affordances live in the header, always reachable, and the sections render only
  when they hold something. An empty day is a header and one sentence.
- **Three formatting rules were consolidated rather than copied.** `formatFigure`
  and `figureUnit` now live in `lib/format.ts` and the Ledger and Metric page use
  them; `today()` moved to `lib/day.ts` out of `annotation-dialog.tsx`. Writing a
  third copy of "a duration_by_state value reads as a duration" in a repo whose
  ADR 0037 was written about rules existing twice seemed like the wrong way round.

One thing seen and deliberately left: a `%` Metric is stored as a fraction, so
`body_fat_percentage` reads "0,3 %" on the Day exactly as it does on every Panel,
Ledger row and Metric page, while the manual entry dialog is the one place that
rescales. Making the Day the second place would have it disagree with the Metric
page it links to. Filed separately.
