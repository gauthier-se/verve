# Seeded default dashboard for a new Account

## Context

A freshly created Account (via the bootstrap of ADR 0017, or the CLI) has no
Dashboards. Dropped onto an empty app, a non-developer has nothing to look at and
no obvious next step. Verve already has full Dashboard/Panel CRUD (ADR 0012); what
is missing is a populated starting point and a path to importing data.

## Decision

At **Account creation**, seed one Dashboard named **"Aperçu"** ("Overview") from a
**dashboard template** — its content defined in Verve, not free-form. The Account
therefore always has a Dashboard on first login. Seeding happens in one place, at
creation, for both the bootstrap and CLI paths.

The template is **one** Dashboard, not several themed ones, with five Panels over
the most universal Metrics (present from an iPhone alone, no Watch required):

- `body_mass` — mass — `latest` — line
- `active_energy` — calories burned — `sum` — bars
- `steps` — `sum` — bars
- `resting_heart_rate` — `average` — line
- `apple_exercise_time` — exercise minutes — `sum` — bars

Sleep is **excluded** for now: its `duration_by_state` aggregation is not yet
wired into the series path, so a sleep Panel would render empty.

Before any import the Panels have no data and render the normal "no data" state;
they fill automatically after the first import. While the Account has no data, the
empty state shows a **CTA to the Import page** (ADR 0016).

## Why

- **Seed at creation, one place.** Guarantees "never an empty screen" and keeps
  the logic off the login path; both account-creation routes get it for free.
- **A template, defined in Verve.** The default content is a curated product
  decision, like the closed Catalog (ADR 0002) — not user input.
- **One Overview, not themed boards.** Several dashboards at once (Training /
  Sleep / Nutrition) overwhelm a fresh Account; the CRUD already lets the user
  build more. One board covers the daily-value Metrics and matches the request
  ("mass, calories burned…").
- **iPhone-universal Metrics.** Panels everyone can populate without extra
  hardware; `active_energy` (not the `total_energy_expenditure` derivative) is the
  plainest reading of "calories burned".
- **Empty Panels are fine.** They double as onboarding — "import to see these
  fill" — and the CTA points straight at the Import page.

## Considered Options

- **Generate on first login (if zero Dashboards):** rejected — same result,
  logic split between login and creation.
- **No seed; a "create from template" button in the empty state:** rejected —
  more effort for the user, against the non-dev-friendly goal.
- **Several themed dashboards seeded at once:** rejected — intimidating for a new
  Account and more template surface to maintain; the user can add their own.
- **Include a sleep Panel:** deferred — `duration_by_state` is not yet in the
  series path; it would render empty.
- **`total_energy_expenditure` for the calories Panel:** rejected as the default —
  the derived active+basal total; "calories burned" reads most naturally as
  `active_energy`.

## Consequences

- Account creation seeds the "Aperçu" Dashboard and its five Panels from a
  template defined in Verve, for both the bootstrap and CLI paths.
- The SPA's empty state (no data yet) shows a CTA to the Import page; Panels use
  the existing "no data" rendering until the first import lands.
- Adding sleep (or a `total_energy_expenditure` Panel) to the template is a later,
  additive change once the underlying aggregation ships.
