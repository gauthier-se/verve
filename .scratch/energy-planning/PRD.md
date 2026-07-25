# PRD — Energy planning (the Plan page)

## Goal

Turn Verve from a place that *shows* health data into one that answers "what should I
eat, and am I doing it?". The Account picks a **Target rate** — how fast it means to
gain or lose body mass — and Verve derives the calorie and macro targets, then judges
the result against what actually happened.

The differentiator is that Verve does not have to guess. It holds 3.5 years of intake
and body mass, which back-compute true expenditure directly. On the reference Account
that matters enormously:

| Figure | Value | Ratio to resting |
| --- | --- | --- |
| Basal estimate (Katch-McArdle) | 1 804 kcal | — |
| **Expenditure estimate, `observed` basis** | **≈ 2 559 kcal** | **1.42** |
| `total_energy_expenditure` (devices) | ≈ 3 530 kcal | 1.96 |

The devices overstate by ~971 kcal/day. An online calculator cannot know this; Verve
can. Getting this wrong is not cosmetic — an Account eating to the recorded figure
would gain weight while its dashboard insisted it was in deficit.

Context, glossary, and the design decisions: `CONTEXT.md` (section **Planning**) and
`docs/adr/0023`. Depends on `.scratch/manual-entry/` for the profile write path.

## What this milestone does

- **A `/plan` page**, in the sidebar beside Data.
- **Basal estimate** — four **Basal equations** the Account selects between:
  Katch-McArdle (`370 + 21.6·LBM`), Cunningham (`500 + 22·LBM`), Mifflin-St Jeor
  (`10·W + 6.25·H − 5·A + s`), Harris-Benedict. Equations whose inputs are missing are
  greyed with the reason. Preselection is the best equation the **trusted** data
  supports (see profile, below).
- **Expenditure estimate** with its **Estimate basis**, resolved best-available and
  always named on screen:
  - `observed` — `mean intake − (Δmass_kg × 7700) / days` over 28 days, mass from a
    **linear regression** (raw weight swings ±1.5 kg day to day; endpoints would be
    noise).
  - `recorded` — mean `total_energy_expenditure`.
  - `predicted` — Basal estimate × activity factor.
- **Target rate slider** — continuous, in % of body mass per week, signed. Named zones
  behind it (aggressive cut · moderate cut · maintenance · lean bulk) as *vocabulary*,
  not buttons. It opens on the current **Phase**'s rate, or — with no open Phase — on
  the Account's **measured actual rate**, so the page starts by saying what you are
  already doing.
- **Derived targets** — calories from the rate against the Expenditure estimate;
  **protein floor** (1.6 g/kg lean mass at maintenance rising to 2.4 in an aggressive
  cut); **fat floor** (~0.6 g/kg body mass, hormonal); carbohydrates as the remainder.
  Protein is presented as a *floor with evidence behind it*; the carb/fat split is
  labelled a **convention**, because at equal calories and protein it has no
  demonstrated effect and Verve should not claim precision it lacks.
- **Phase** — persisted with full history. Opening one closes the current; at most one
  open. Each carries its target rate and its window.
- **Adherence** over the open Phase's real window: target vs actual for rate, intake
  and protein.
- **Guardrails that warn, never block** — target below the Basal estimate; a rate
  beyond ~1%/week held over ~8 weeks; an aggressive cut with protein under the floor.
  Verve does not know the Account's medical context, the same reason it refuses to
  colour deltas good/bad (ADR 0015).
- **Profile editor**, on the same page: date of birth and biological sex (Account
  columns, both empty on the reference Account), height / mass / body-fat via
  **Manual entry**, and a **body-composition trust** setting —
  measured (DEXA, hydrostatic) · estimated (bioimpedance scale) · unknown. "Estimated"
  demotes the lean-mass equations in the picker without forbidding them. Default:
  *estimated* when the data comes from a Connector, *measured* when manually entered.

## What this milestone does NOT do

- **Estimates are never Metrics.** They are computed on read and stay out of the
  Catalog, so they never appear in a Panel or the Ledger beside observations
  (ADR 0023).
- **No auditing of the Account's body-composition source.** Verve *can* detect a
  useless bioimpedance figure — the reference Account's body fat tracks body mass at
  r = 0.9895 over 474 days — but the trust setting is a declaration, not an
  inference. Recorded here because the temptation will recur.
- **No lean-mass adherence.** Deliberately excluded: with that correlation, losing
  6 kg mechanically renders −1.9 kg of "lean mass" whether or not any muscle was
  lost. Reporting a fabricated failure on the most consequential variable is worse
  than reporting nothing.
- **No meal planning, food database, or logging.** Intake comes from the existing
  nutrition Metrics.
- **No blocking.** Every guardrail is a warning.
- **No Phase annotations on Panels.** Natural next step; not this milestone.

## Issues

1. `01-estimates-engine` — server: Basal equations, the expenditure cascade, the
   regression-based mass trend.
2. `02-phase-model-and-api` — data + api: `phases` table, lifecycle, targets and
   adherence endpoint.
3. `03-profile-and-trust` — data + api + web: date of birth, biological sex,
   body-composition trust; profile editing over Manual entry.
4. `04-web-plan-page` — web: the page, equation picker, basis disclosure, rate slider,
   targets, adherence, guardrails.
