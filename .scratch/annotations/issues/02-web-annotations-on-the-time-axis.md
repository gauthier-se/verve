Status: done

# 02: web: markers and bands on the time axis

## What

- **`useAnnotations(tokens)`** in `web/src/lib/` beside the existing query hooks:
  one `GET /v1/annotations` per resolved time axis, keyed on the same tokens the
  Series queries use, so a Dashboard fetches its Annotations once and its Panels
  share the cache rather than each fetching its own. Types in `lib/types.ts`.
- **`panel-chart.tsx` renders them behind the marks**, before the `marks(...)`
  map so the data always draws on top:
  - no `end_bucket` on the payload means a marker: `ReferenceLine x={bucket}` with
    the recessed muted/dashed treatment `baselineLine` already establishes
    (`panel-chart.tsx:201`), never a saturated colour, because an Annotation is
    context and not a series. Its presence means a band: `ReferenceArea
    x1={bucket} x2={end_bucket}` in the same muted tone at low opacity, no
    border. The server emits `end_bucket` only when it differs from `bucket`, so
    that test needs no comparison and no special case for a fortnight that folds
    into a single month bucket.
  - **A Series is sparse**: its points come from a `GROUP BY`, so a bucket with no
    data is absent from the payload and therefore absent from the axis'
    categories, and Recharts matches `x` against a category by equality. Place
    the marker on the first category `>= bucket` (a lexical comparison over
    `YYYY-MM-DD` strings, which is chronological, and still no date arithmetic);
    when there is none, the note is past the last drawn bucket and draws nothing.
    The tooltip always names the real `starts_on`/`ends_on`, so a marker nudged
    onto the next drawn bucket never misstates when the thing happened. The real
    fix is a dense Series, which is a change to the read path and not to this
    milestone (ADR 0030).
  - a small label-free tick at the top of the plot area marks each Annotation, and
    **several in one bucket collapse to one tick carrying a count** (`3`), because
    the labels themselves belong to the tooltip and stacked text at bar width is
    illegible by the second one.
  - Nothing renders when the list is empty: no legend row, no reserved band, no
    layout shift on a Dashboard that has no Annotations.
- **The tooltip gains a section**, in `ChartTooltip`: the bucket's values as
  today, then a separator, then the labels of the Annotations covering that
  bucket. One hover target, not two. A span's label repeats on every bucket it
  covers, which is what makes a band readable at all.
- **The toggle** in the Dashboard's time-axis controls, beside `ComparisonControl`
  (`comparison-control.tsx:18`): a single on/off patching `annotations` on the
  Dashboard, with the same optimistic-then-invalidate shape the comparison and
  range controls use. Hidden when the Account has no Annotations at all, a switch
  for something that does not exist teaches nothing.
- **The Metric page** renders markers on the same terms, its toggle local React
  state defaulting to on: a Metric page has no persisted time axis (ADR 0025), so
  it has nowhere to store one and must not grow a store for it.
- **A contract test**, Go-side in `internal/web/annotations_test.go`, in the shape
  `workouts_test.go` and `sleepstages_test.go` established: the SPA is read as
  text, so it runs in `make ci` with no front-end toolchain. Assert the two things
  that fail silently rather than loudly:
  - `panel-chart.tsx` places its markers on the server's `bucket` field and does
    no date arithmetic of its own near the reference elements, no `startOfWeek`,
    no `startOfMonth`, no `date-fns` bucket helper. A client-computed date that
    disagrees with the grid by one boundary rule renders nothing at all, with no
    error anywhere.
  - the Annotation overlay is emitted before the `marks(...)` call, so the data is
    never drawn behind the context.

## Why here

The server already answered "which bucket" (issue 01), so this component's only
job is to draw at a categorical X value it was given. Any arithmetic on dates in
this file is a bug: Recharts matches `ReferenceLine x` against the axis category
by equality, so a client-computed date that is one boundary rule away from the
server's silently renders nothing, the failure mode is an invisible marker, not
an error, which is exactly why the computation belongs on the other side.

Drawing behind the marks and in the Baseline's tone is the whole visual rule: a
Panel already carries up to four series and a Baseline overlay (ADR 0020), and an
Annotation that competed with any of them would make the chart about the note
instead of about the data.

## Comments

Shipped. `web/src/lib/annotations.ts` (the projection), `web/src/hooks/use-annotations.ts`,
the overlay and tooltip section in `panel-chart.tsx`,
`web/src/components/annotations-control.tsx`, the `showAnnotations` thread through
`dashboard-view` → `dashboard-grid` → `panel-card`, the Metric page's local toggle,
and `internal/web/annotations_test.go`.

Three notes on what differs or was learned:

- **The projection lives in `lib/annotations.ts`, not in the chart.** Placing a note
  on a category is decidable from two string comparisons and benefits from being
  read on its own; `panel-chart.tsx` only draws what it is handed. The contract test
  covers both files, so the ban on date arithmetic holds either side of the seam.
- **No separate tick row.** The spec asked for a label-free tick per Annotation at
  the top of the plot area, collapsing to one with a count. The dashed rule already
  is that mark, and a tick above it would be a second glyph saying the same thing,
  so there is one rule per bucket carrying a count when several notes land on it.
- **The toggle gates the render, not only the fetch.** A react-query `enabled:
  false` keeps serving whatever it cached while it was enabled, so gating only the
  hook would have left the markers on screen after switching them off. Both call
  sites pass `undefined` when the toggle is off.

Verified: `npx tsc --noEmit`, `npm run build`, `make ci-go`, and the four contract
tests, each checked to actually fail when its rule is broken. Also smoke-tested
against the real binary over HTTP: a 5-12 August span folds to `2026-08-03` /
`2026-08-10` at the week bucket (two ISO weeks, a band) and to a single
`2026-08-01` with no `end_bucket` at the month bucket (one marker), and the
Dashboard payload carries `annotations: true`.

**Not verified: the rendering itself.** This environment has no browser and the
repo has no front-end test runner by design, so nothing here has been seen on
screen, and `projectAnnotations` has no runtime test. The Go contract tests hold
the rules that fail silently; they cannot tell you a band looks right. If a
front-end runner is ever wanted, this function is the argument for it.
