Status: done
Blocked by: 01, 02

# 03: web: the list that outlives an import, and the delete that means it

## What

- **`web/src/hooks/use-exclusions.ts`**, following `use-pins.ts`: `useExclusions()`,
  `useCreateExclusion()`, `useDeleteExclusion()`. A create invalidates the
  Exclusions, the series, the ledger, the history and the import status: it just
  deleted rows, so every read of the data is stale, which is not true of any
  other write in this app and is worth an explicit comment on the invalidation
  list.
- **Types** in `lib/types.ts`: `Exclusion`, and `excluded` on the import report.
- **The Import page carries the list** (`import-page.tsx`), between the `Steps`
  rail and the drop zone, as a card titled for what it is: what the import you
  are about to run will refuse. One row per Exclusion, "Body fat percentage,
  from 1 Jan 2026" / "Body fat percentage, everything", a remove control on each,
  and an "Exclude a metric" control opening the dialog below. The card is absent
  when the set is empty, so a first import is the screen it is today.
  Under the list, one line: removing an exclusion restores nothing by itself, the
  next import brings back whatever the export still holds. That sentence is the
  feature's undo, and it is the only place it can be read.
- **The exclusion dialog** (`exclusion-dialog.tsx`), reusing `MetricPicker` for
  the Metric and `DayRangePicker` for the optional span, both already built.
  Defaults: no dates, which is "everything this Metric holds", the common case.
  It is the same dialog from both surfaces, and it always shows, before the
  confirm, how many Measurements the Account currently holds under it, from the
  count endpoint issue 01 adds. A number is the only thing that makes the
  difference between "exclude going forward" and "delete 431 readings" legible
  in the half-second before the click.
- **The Metric page carries the delete** (`metric-page.tsx:80`). The header is
  already full (`PinToggle`, notes, grain, presets), so the action goes in an
  overflow `DropdownMenu` at its end rather than as a fifth visible button:
  "Delete this metric's data", destructive styling. It opens the same dialog with
  the Metric pre-filled and locked, and the range pre-filled from the page's
  current preset **only if the owner picked a custom range**: a "3m" preset is
  how you were reading the chart, not a statement about what to delete, and
  pre-loading it into a destructive dialog would be a trap. Default to no bounds.
  On success, the page's queries refetch and the chart empties, which is the
  confirmation. No toast claiming something the screen does not already show.
- **The confirmation is typed, not clicked.** The dialog's destructive step names
  the Metric, the span and the count, states that Manual entries in the span go
  too and are not restorable, and requires the Metric's own label to be typed
  before the button enables. Every other destructive action in this app removes
  one row, one Panel, one Dashboard; this one removes years, and the export is
  the only backup.
- **The report card gains its fourth number** (`import-page.tsx:ReportCard`). Not
  a fourth cell in the three-column grid: a quiet card under it, in the shape of
  `UnmappedCard`, saying how many records the exclusions refused and under which
  Metrics, rendered only when `excluded > 0`. Unmapped means "the Catalog could
  not read this"; excluded means "you told me not to", and the two must not read
  as the same kind of leftover.
- **The History renders the event** (`history-page.tsx`): `exclusion` in the
  event colour map, an icon (`Ban` or `EyeOff`, whichever the icon set has, not a
  trash can: this is a standing refusal, not a one-off delete), and its entry in
  the explanation `switch` at line 318, in the voice of its neighbours: what was
  excluded, how much it removed, and that the next import will refuse it again,
  which is why the number does not come back.
- **Tests** where the web tests already are, matching the annotations milestone's
  coverage: the list renders and removes, the dialog refuses an empty Metric,
  the confirm button stays disabled until the label is typed, and the excluded
  card appears only with a non-zero count.

## Why here

The list lives on the Import page rather than in a settings screen because the
request behind this feature is "when I come back it proposes this import". A set
of rules read on the page where the import starts, seconds before it starts, is
that proposal. Filed under settings it would be a thing to remember to check,
which is the opposite.

The typed confirmation is not house style, and it is deliberate. `DELETE
/v1/measurements/{id}` removes a value someone typed a minute ago; this removes
what a device recorded over years, and the difference has to be felt in the
gesture, not stated in a sentence under it.

The count shown before the delete is the same query the purge will run
(`CountByMetric` and `DeleteByMetric` share their predicate, issue 01), so the
number on screen is the number that goes. Two predicates that could drift would
turn the confirmation into an estimate, and an estimate is not a confirmation.

## Comments

Shipped. `web/src/hooks/use-exclusions.ts`, `web/src/components/exclusion-dialog.tsx`,
the `Exclusions` card and the `ExcludedCard` on the import page, the overflow menu on
the Metric page, the `exclusion` event on the History page, and the types. ROADMAP
gained its shipped row. Typecheck and production build are clean.

The milestone is complete, and it was smoke-tested on the real binary rather than only
in tests: import four records, exclude `body_fat_percentage` from 2026-01-01 (preview
says 2, the purge removes 2), re-import and watch the CLI report
`0 added 1 skipped 2 excluded`, delete the rule, re-import, and the two rows come back.
The `sleep` and `calorie_balance` refusals answer 422 from the preview, before the
click rather than after it.

Four things came out of the implementation that the spec did not anticipate:

- **The span is two date inputs, not `DayRangePicker`.** That component commits only
  when both ends are chosen (`onSelect(from, to)`), and an Exclusion's whole common
  case is an open bound: "from 2026" and "everything" are the two things people
  actually type. Two inputs, the shape the annotation dialog already uses, say that;
  a range picker cannot express it without being rewritten.
- **The confirmation scales with what is at stake.** The spec asked for a typed
  confirmation always. It is now required only when the preview finds rows to purge:
  excluding a Metric that has nothing stored destroys nothing, and asking someone to
  type "Body fat percentage" to do nothing teaches them to type it without reading.
  With rows at stake the friction is exactly where the spec wanted it.
- **The preview's 422 is shown in the dialog, live.** It validates what the create
  validates, so a derived or state-backed slug is refused as it is picked rather than
  after the confirm. The Metric page also hides the menu entry entirely for those
  Metrics, so the refusal is a backstop rather than the first thing anyone meets.
- **The empty state is a quiet button, not the card.** An Account with no Exclusion
  sees one ghost control above the drop zone rather than an empty panel, so a first
  import is the screen it has always been.

Two things the spec asked for that were not done, and why:

- **No web tests.** The `web/` package has no test runner: no vitest, no `test`
  script, and no existing `*.test.tsx` anywhere. `npm run typecheck` and the
  production build are what this repo checks a front-end change with today. Adding a
  runner is a decision of its own and not a side effect of this issue.
- **The `EVENT_COLOR` entry is the destructive colour**, not a ramp colour. An
  exclusion is the one event on that page that took data away, and the destructive
  colour is one of the two Palettes deliberately keep identical because it carries
  meaning rather than style (ADR 0024). Every other event is something that arrived
  or was decided; this one is the gap the page exists to explain.
