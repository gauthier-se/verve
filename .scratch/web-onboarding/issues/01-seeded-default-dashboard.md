# 01 — Seeded default dashboard + shared account-creation path

Status: ready-for-agent
Blocked by: —

## Goal

Give every new Account a populated starting point — one "Aperçu" Dashboard seeded
at creation — so no Account faces an empty app. Do it behind a **single reusable
account-creation path** shared by the CLI and (later) the web bootstrap, and ship
it first through the CLI where it is verifiable today.

## Scope

- **Prefactor — shared creation path.** Account creation is currently inline in
  the CLI `account create` command. Extract "insert Account, then seed its default
  Dashboard" into one reusable operation that any caller (CLI now, web bootstrap in
  #02) invokes, so seeding cannot be forgotten by a caller.
- **Dashboard template.** Define in Verve (curated, not user input) one Dashboard
  named **"Aperçu"** with five Panels, seeded in this order, reusing the existing
  Dashboard/Panel insert path (ADR 0012):
  - `body_mass` — `latest` — line
  - `active_energy` — `sum` — bars
  - `steps` — `sum` — bars
  - `resting_heart_rate` — `average` — line
  - `apple_exercise_time` — `sum` — bars
- The seeded Dashboard is thereafter an ordinary Dashboard (editable, deletable).

## Out of scope

Web bootstrap endpoint and screen (#02). Web import (#03). A sleep panel
(`duration_by_state` not yet in the series path). A `total_energy_expenditure`
variant for the calories panel.

## Acceptance

- [ ] `verve account create --email=…` inserts the Account **and** an "Aperçu"
      Dashboard with the five Panels above, in order.
- [ ] Logging in through the existing SPA shows "Aperçu" with five "no data"
      Panels (no data until an import lands).
- [ ] The template content is defined in one place in Verve, not derived from user
      input.
- [ ] The seeded Dashboard behaves as a normal Dashboard afterward (rename, edit
      panels, delete).
- [ ] Both the CLI path and any future caller go through the same shared creation
      operation.

## Refs

ADR 0018, ADR 0012, ADR 0002. CONTEXT.md: Dashboard template, Dashboard, Panel.
`cmd/verve/commands.go` (`accountCreate`), `internal/data/dashboard.go`.
