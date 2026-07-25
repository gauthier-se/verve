# Estimates are computed on read, never Metrics — and the observed basis wins

## Context

Planning a cut, a bulk or a maintenance stretch needs a number Verve does not have:
how much energy the Account actually spends in a day. Two candidates already exist
in the Catalog and **both are wrong for the job**.

`basal_energy` is Apple's own resting figure, imported, 262k points on the reference
Account. `total_energy_expenditure` is a derived Metric, `active_energy +
basal_energy` (ADR 0014). Measured against the reference Account's own 28-day
history, they say this:

| Figure | Value | Ratio to resting |
| --- | --- | --- |
| Katch-McArdle from the Account's lean mass | 1 804 kcal | — |
| **Back-computed from intake vs body-mass trend** | **≈ 2 559 kcal** | **1.42** |
| `total_energy_expenditure` (what the devices recorded) | ≈ 3 530 kcal | 1.96 |

The devices overstate expenditure by roughly **971 kcal/day**. A ratio of 1.96 is
the activity level of a professional endurance athlete; 1.42 is a normally active
adult. Meanwhile mean intake was 2 078 kcal/day and body mass fell 92.75 → 91.00 kg
over the window, which only reconciles with a true expenditure near 2 559. An
Account eating to the recorded figure would gain weight steadily while its dashboard
insisted it was in deficit.

So the planning figures are inferences about the Account, not observations of it.
The question is where they live and which evidence produces them.

## Decision

Introduce **Estimates**: quantities Verve infers, computed on read, **never stored
as Measurements and never entered in the Catalog**. A Metric answers "what was
measured"; an Estimate answers "what is true", which the measurement may approximate
badly. Keeping Estimates out of the Catalog keeps them out of Panels and the Ledger,
where they would sit beside observations and be read as one.

Two Estimates, deliberately named apart from the Metrics they must not be confused
with:

- **Basal estimate** — resting expenditure from a **Basal equation** the Account
  picks (Katch-McArdle, Cunningham, Mifflin-St Jeor, Harris-Benedict). Not
  `basal_energy`.
- **Expenditure estimate** — total daily expenditure, the figure calorie targets are
  built on. Not `total_energy_expenditure`.

Every Expenditure estimate carries its **Estimate basis**, resolved best-available
and **always named on screen**:

1. `observed` — back-computed from logged intake against the body-mass trend:
   `mean intake − (Δmass_kg × 7700) / days`, over a 28-day window with mass taken
   from a linear regression rather than endpoints.
2. `recorded` — the mean of `total_energy_expenditure`.
3. `predicted` — Basal estimate × a chosen activity factor.

**`observed` outranks `recorded`** — Verve deliberately trusts what the body did over
what the devices claim.

## Considered Options

- **Estimates on read, observed basis first (chosen).** The observed basis is the
  only one grounded in an outcome rather than a model, and it is the one basis that
  needs no body-composition input at all — which matters, because that input is the
  least trustworthy datum in the account (ADR 0022).
- **Model the Basal estimate as a derived Metric.** Superficially the natural home,
  and it does not fit: Mifflin-St Jeor is `10·W + 6.25·H − 5·A + s`, which needs an
  additive constant and two operands (age, sex) that are Account attributes, not
  Metrics. A Formula is a ratio of weighted sums (ADR 0014) and is *deliberately* not
  a general expression. Widening it to host one equation would trade a small closed
  language for an open one, and would put a theoretical figure on the same axis as
  measured data.
- **Trust the recorded basis.** Zero new arithmetic, and wrong by ~38% on the only
  real dataset available. Rejected on the evidence.
- **Ask the Account for an activity factor (the online-calculator approach).**
  Ignores 3.5 years of intake and body-mass history that answer the question
  directly. Kept only as the `predicted` fallback for an Account with no history.

## Consequences

- The Plan page can be honest about provenance: the basis is part of the answer, and
  an Account seeing `recorded` knows the figure is a device claim.
- The observed basis needs both `dietary_energy` and `body_mass` densely enough over
  the window; the cascade exists precisely so a thin Account still gets a figure.
- **7700 kcal/kg** is the assumed energy density of body-mass change. It is a fair
  approximation for fat loss and a poorer one for a bulk, where accrued tissue is
  mixed. This is a known, accepted inaccuracy, not an oversight.
- Because Estimates never become Metrics, there is no migration and no Catalog
  change. Should they ever need to be graphed, that is a separate decision with a
  separate cost — this ADR does not foreclose it, it just refuses to pay for it now.
- The Basal estimate is, for an Account whose body composition comes from a
  bioimpedance scale, the weakest figure on the page. It is displayed because the
  Account asked for it and because `predicted` needs it; it does **not** feed the
  calorie target when a better basis exists.
