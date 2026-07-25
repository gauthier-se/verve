# 03 — Profile: date of birth, biological sex, body-composition trust

Status: ready-for-agent
Blocked by: `.scratch/manual-entry/` issue 02

## Goal

Let the Account fill in what the import did not. On the reference Account
`date_of_birth` is NULL and `biological_sex` is empty, which makes Mifflin-St Jeor and
Harris-Benedict uncomputable — and those are precisely the equations that matter when
the body-composition data is a bioimpedance estimate.

## Scope

- **Migration**: `accounts.body_composition_trust TEXT NULL` —
  `measured` | `estimated` | `unknown`.
  `date_of_birth` and `biological_sex` already exist (`internal/data/account.go`);
  they are nullable and currently written only by the Apple `Me` import.
- **`AccountModel.UpdateProfile(ctx, id, fields)`** — partial update of
  `date_of_birth`, `biological_sex`, `body_composition_trust`, bumping `updated_at`.
  Follow `SetPassword` for the shape. Never touches `email` or `password_hash`.
- **`GET /v1/profile`** / **`PATCH /v1/profile`** (`requireAuth`)
  - `date_of_birth` — `YYYY-MM-DD`, must be in the past and yield an age in 13–120;
    else 422.
  - `biological_sex` — `male` | `female` | unset. It exists **only** as an input to
    two equations, and the UI must say so rather than implying Verve models gender.
  - `body_composition_trust` — the three values above.
  - The response also reports the **derived default** for trust when it is unset:
    `estimated` when lean mass / body fat come from a Connector, `measured` when the
    most recent such value is a **Manual entry**. Manual entry already expresses a
    judgement, so it is trusted by default; a scale is not.
  - Reads reuse `GET /v1/auth/me`'s Account resolution — do not add a second lookup
    path.
- **Effect on equation preselection** (consumed by issue 01/04): trust `estimated` or
  `unknown` **demotes** Katch-McArdle and Cunningham below Mifflin-St Jeor in the
  preselection order. They stay selectable — demoted, never hidden.
- **Metric-valued profile fields** — height, body mass, body fat are **not** columns.
  They are written through `POST /v1/measurements` (Manual entry, ADR 0022). This
  issue owns only the Account columns; the Plan page's profile section composes the
  two write paths behind one UI (issue 04).

## Out of scope

- Adding `height` as an Account column. Explicitly rejected in ADR 0022: it would
  create a second height that silently diverges from the Metric.
- Inferring trust from the data. Verve holds the evidence — the reference Account's
  body fat tracks body mass at r = 0.9895 — and still asks (PRD, "does NOT do").
- Blood type, and the rest of Apple's `Me`.

## Acceptance

- `PATCH /v1/profile` sets each field independently; omitted fields are untouched.
- Setting `date_of_birth` makes Mifflin-St Jeor computable in `GET /v1/plan` without
  any other change — the two issues meet here, and a test should cross that seam.
- An out-of-range date of birth and an unknown sex value each yield 422.
- Trust unset returns the derived default with an indication that it is derived, so
  the UI can show it as a suggestion rather than a stored choice.
- Trust `estimated` puts Mifflin-St Jeor first in the preselection order when age and
  sex are known; `measured` puts Katch-McArdle first.
- Profile changes never affect another Account.

## Refs

ADR 0022 (Manual entry, and why height is not a column), ADR 0023.
CONTEXT.md: **Account**, **Basal equation**, **Manual entry**.
`internal/data/account.go`, `internal/data/migrations/`,
`internal/api/authhandlers.go` (`handleMe`).
