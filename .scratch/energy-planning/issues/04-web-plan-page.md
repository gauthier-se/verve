# 04 — Web: the Plan page

Status: done
Blocked by: 02, 03

## Goal

One page that answers "what should I eat, and am I doing it?" — and that is honest
about how confident each of its numbers deserves to be.

## Scope

- **Route `/plan`** in `router.tsx`, sidebar entry in `app-shell.tsx` beside Data.
- **Expenditure estimate — the headline.** Large figure, and **always** the basis
  beside it in plain words:
  - `observed` → "from your intake and weight trend, 28 days"
  - `recorded` → "from your devices" — plus a caution that recorded expenditure often
    overstates (on the reference Account by ~971 kcal/day)
  - `predicted` → "estimated from an equation"

  The basis is not a footnote. It is the difference between a number worth eating to
  and one worth ignoring.
- **Basal estimate — equation picker.** Four options with their values; uncomputable
  ones greyed with the missing input named ("needs your date of birth") and a link to
  the profile section. Preselection comes from the server (issue 03), not from client
  logic.
- **Target rate slider** — continuous, signed, % of body mass per week. Named zones
  rendered as background bands (aggressive cut · moderate cut · maintenance · lean
  bulk): labels, not snap points. Opens on the current Phase's rate, else on the
  measured actual rate. Show the rate three ways at once — %/week, kg/week, and the
  resulting calorie target — because the first is precise, the second is intuitive,
  and the third is what gets acted on. Debounce `GET /v1/plan?rate=` while dragging.
- **Targets** — calories, protein, fat, carbohydrates. Protein carries its rationale
  (a floor that rises with the deficit, to protect lean mass). **Carbohydrates and fat
  are labelled a convention**, visually subordinate to protein. Reuse
  `formula-hint.tsx`'s tooltip pattern, which already explains derived Metrics.
- **Phase** — open / close, and the history as a compact list of past phases with
  their windows and rates.
- **Adherence**, when a Phase is open: target vs actual for rate, intake, protein, over
  the Phase's real window. **Neutral presentation, never coloured good/bad** — the same
  rule as Baseline deltas (ADR 0015). A window under ~14 days is marked as thin rather
  than being suppressed.
- **Guardrails** — rendered as inline warnings from the server's structured list.
  Nothing is ever disabled.
- **Profile section**, on the same page: date of birth, biological sex (labelled as an
  input to two equations, nothing more), body-composition trust, and height / mass /
  body fat via Manual entry. One UI over two write paths (`PATCH /v1/profile` and
  `POST /v1/measurements`).
- **Empty states that route somewhere.** A fresh Account should read "import your data
  or fill in your profile", never a blank card or a zero.
- **FR formatting** — comma decimals, space thousands, as `format.ts` already does.

## Out of scope

- Charting the estimates over time. They are not Metrics (ADR 0023).
- Editing a past Phase's window.
- Any recomputation of targets client-side — render what `/v1/plan` returns.
- Lean-mass adherence. Deliberately absent (PRD).

## Acceptance

- The page renders end to end against a seeded Account and names the basis in words.
- Dragging the slider updates calories and macros without a full-page refetch; the
  rate reads simultaneously as %/week, kg/week and kcal.
- With `date_of_birth` NULL, Mifflin-St Jeor is greyed and names the missing input;
  filling it in on the same page enables it without a reload.
- Opening a Phase closes the previous and adherence begins from the new start date.
- Guardrails appear as warnings, and every control stays usable.
- Adherence figures are neutral — no red/green — matching the Baseline delta rule.
- Setting body-composition trust to "estimated" moves Mifflin-St Jeor to the top of
  the picker without hiding Katch-McArdle.
- A fresh Account with no data shows a routed empty state, not zeros.

## Refs

ADR 0023, ADR 0015 (neutral feedback), ADR 0013 (SPA), ADR 0019.
CONTEXT.md: section **Planning**.
`web/src/router.tsx`, `web/src/components/app-shell.tsx`,
`web/src/components/panel-summary.tsx` (headline-figure pattern),
`web/src/components/formula-hint.tsx` (explanatory tooltip),
`web/src/lib/format.ts`, `web/src/components/ui/`.

## Comments

Implemented on branch `feat/manual-entry`.

- `web/src/components/plan-page.tsx`, `web/src/hooks/use-plan.ts`, route `/plan`, sidebar
  entry beside Data. Includes issue 03's profile UI, which was always specified to live
  here.
- **The basis is a sentence, not a badge.** Each of the three gets its own explanation, and
  `recorded` carries the caution that devices overstate — the page says so at the moment it
  shows the number, not in a help article.
- The slider is **uncontrolled until touched**: until then the server decides the rate (the
  open Phase's, else the measured actual), so the page opens by stating what the Account is
  already doing. `placeholderData` keeps the previous payload on screen while a new rate is
  in flight, so dragging updates figures instead of flashing a spinner over them.
- Protein is rendered as a solid card, fat and carbohydrate as dashed and muted, with the
  convention stated underneath. Three equal-looking numbers would claim an authority the
  split does not have.
- Guardrails render as advisories; nothing is ever disabled. Adherence is neutral — no red,
  no green (ADR 0015).

### Three defects only the browser found

`tsc --noEmit`, `vite build` and the whole Go suite were green while the page **crashed on
load**. Caught by screenshotting it.

1. **`plan.guardrails` arrived as `null`, not `[]`.** Go's `Guardrails()` returned a nil
   slice when nothing fired, which marshals to `null`, and the client reads `.length`. So
   the page broke on the *happy path* — the one case where nothing is worth warning about.
   Fixed server-side (the contract declares an array) and pinned by
   `TestGuardrailsAreNeverNil`.
2. **The zone labels claimed positions they did not occupy.** Spaced with
   `justify-between`, "Maintenance" sat at the centre of the track while the maintenance
   zone is nowhere near it. Each label is now sized to its own zone's span, with dividers.
3. **kg/week was being back-computed on the client** from the calorie gap — arithmetic on
   the client is arithmetic nobody tests (ADR 0019). `Targets` now carries `kg_per_week`
   from the server, which has the body mass to convert with.

Also moved the adherence Target/Actual legend above the rows it labels rather than below.

### Verification

Screenshots plus a scripted interaction on a throwaway DB with 28 seeded days:

```
slider → −1.1 %   both guardrails shown (below-basal, unsustainable), nothing disabled
start a phase     phase opens, adherence appears, short window flagged, history appears
profile dob+sex   all four equations become computable (1804 / 1961 / 1922 / 2030)
trust estimated   Mifflin-St Jeor preselected, Katch-McArdle still shown with its figure
runtime errors    none
```

Note on the scripted checks: several early "absent" results were false negatives from
case-sensitive regexes against `innerText`, which returns CSS-uppercased headings. Worth
knowing before trusting a similar script.
