# 04 — Web: the Plan page

Status: ready-for-agent
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
