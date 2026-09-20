Status: done
Blocked by: 02

# 03: web: a day bucket is a link, and the docs say so

## What

- **`PanelChart` gains `onSelectBucket?: (bucket: string) => void`**, symmetric
  with the `onHoverBucket` it already carries, wired to the bar and point
  handlers. `PanelCard` passes it only when the resolved grain is `day`, so the
  chart itself stays ignorant of routing and of which Metric it is drawing.

- **Only at day grain.** A week or month bucket has no Day, and clicking one
  does nothing rather than navigating to its first date, which would be a guess
  presented as a destination. The cursor states differ accordingly, so the
  difference is visible before the click and not after it.

- **The Ledger detail table** (`ledger-detail-table.tsx`): each dated row
  navigates to its Day, at day grain only, the way the sleep Metric's rows
  already navigate to their Night. The sleep rows keep going to the Night: they
  address one already, and a Night is a better answer than the Day that contains
  it.

- **The History band**: a day on the band navigates to its Day. The band is
  dense by construction (ADR 0032), so an empty day is clickable too and lands
  on the empty Day page, which is the correct answer and the reason issue `02`
  designs that state.

- **The docs pass**, which rides here because every claim it touches is about
  navigation and not about either half alone:
  - **ADR 0043**, a Day is a bucket read as an index, not an entity: opening on
    ADR 0034, which made the day a unit of decision before anything displayed
    it; the three composition rules (the Night is the one labelled by the date,
    a Session belongs to the day it started, an empty date is a 200); and the
    refusals, an intra-day axis on the Day, a sub-day bucket, a Day as a Metric,
    a score, a comparison, and a week or month page.
  - **ADR 0041** gains a pointer: the Day is the case that proves its rule
    rather than the exception to it, addressable and still carrying no axis.
  - **ADR 0034** gains a pointer: the per-day Source election now has one place
    where it is visible.
  - **CONTEXT.md**: the **Day** entry beside **Night**, with its Avoid list, and
    a clause on **Night** naming which Day shows it.
  - **README.md**: a bullet under "Look at it properly", and a clause each on
    the Sleep and Workouts bullets saying the Day reaches them.
  - **ROADMAP.md**: a shipped row.

## Why here

**This is the feature.** The page in issue `02` is worth building because the
most common gesture in the app, pointing at a bucket, stops being inert. Shipped
without these links the Day is a page nobody finds, reachable only by typing a
URL.

**The `PanelChart` objection is answered by uniformity.** The hypnogram
milestone declined to make chart bars navigate, and was right to: teaching a
shared chart that one Metric is special is coupling. This is the opposite case.
Every Panel at day grain navigates to a Day, no Metric is privileged, and the
condition is the grain the chart already knows. The callback matches the hover
one, so the component gains a second event and no knowledge.

**The grain check is the whole safety property.** It is what stops "clicking a
bucket opens it" from becoming "clicking a month opens a month page", which is
the week and month page ADR 0043 refuses.

## Comments

Shipped. `useDayNavigation` in `hooks/use-day.ts`, `onSelectBucket` on
`PanelChart`, the three call sites (`panel-card.tsx`, `metric-page.tsx`, the
History band), the Ledger row link, a source-level pin in
`internal/web/daynavigation_test.go`, and the docs pass: the pointers on ADR 0041
and ADR 0034, the **Day** clause on CONTEXT.md's **Night** entry, the README
bullet with its clauses on Sleep and Workouts, and the shipped ROADMAP row.

Verified against the real import rather than only typechecked: a dashboard panel
at day grain navigates (clicked, landed on `/days/2026-06-07`), the same panels at
week grain do not, a Metric page bar at day grain navigates and at month grain
does not, the Ledger rows carry the link at day grain and render plain above it,
and the History band navigates at day grain, checked on a second Account seeded
with twelve days so the band resolves to one.

Four things the spec did not anticipate:

- **The cursor could not be set with a class.** Recharts writes
  `cursor: default` inline on its own wrapper, so a Tailwind `cursor-pointer`
  loses to it silently: the class was on the element and the computed style was
  still `default`. Its `style` prop is spread after that default, so the
  affordance goes there. This is the kind of thing that only shows up in a
  browser, and it is the whole of "the difference is visible before the click".
- **Sleep is not special-cased on the charts, and the spec's wording was wrong
  about it.** The issue said the sleep Metric's bars "keep going to the Night";
  they never went anywhere, because the hypnogram milestone deliberately declined
  to teach a shared chart that one Metric is different. That decision still holds,
  so a sleep bar goes to the Day like every other bar, and the Day names the night
  and links to its shape. One more click, no coupling. The Ledger's sleep *rows*
  do keep their Night link, which is what the issue was reaching for: they address
  a Night already, and a Night is a better answer than the Day containing it.
- **The gate is one hook, not a check per call site.** Three charts and a table
  all ask the same question of the same grain. `useDayNavigation(bucket)` returns
  the callback or `undefined`, and returning `undefined` rather than a no-op is
  what lets each caller render the difference.
- **The grain rule is pinned as a source contract** (`daynavigation_test.go`).
  Nothing fails at runtime when it drifts: a chart handed an unconditional
  callback navigates perfectly well, to the wrong place, and the only symptom is a
  reader who clicked on August and landed on the 1st. The pin also refuses a new
  component that links to `/days/$date` without going through the hook, so the
  next screen that wants one has to answer the grain question. Both halves were
  mutation-checked: removing the gate and dropping the prop each fail it.

One stated deviation: the spec put ADR 0043 in this docs pass, and it landed with
issue `01` where the decision it records was made, the way ADR 0041 landed with
issue `01` of the intra-day milestone. This pass carries everything else.
