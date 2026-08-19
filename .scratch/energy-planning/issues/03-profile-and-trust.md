# 03 — Profile: date of birth, biological sex, body-composition trust

Status: done

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

## Comments

Server side implemented on branch `feat/manual-entry`, **folded into issue 02** — see that
issue's comments for why (preselection needs the trust column, so shipping 02 first would
have meant writing a stub).

Delivered here: migration `0009_body_composition_trust`, `AccountModel.UpdateProfile` with
a partial `ProfilePatch`, `GET` / `PATCH /v1/profile`, and `estimate.Preselect` /
`estimate.DerivedTrust`.

- `ProfilePatch` uses double pointers so a partial update can distinguish the three cases
  that matter: absent (leave alone), null (clear), and a value (set). A client sending one
  field cannot blank the others by omission — pinned by
  `TestUpdateProfilePatchesNamedFieldsOnly`.
- Preselection **demotes, never hides**: with trust `estimated` or `unknown`, Mifflin-St
  Jeor leads and the lean-mass equations follow, but they stay selectable and their figures
  stay on screen. `TestPreselectFallsBackToWhatIsComputable` covers the reference Account's
  real case — age and sex absent, so even distrust leaves Katch-McArdle as the only thing
  that can run.
- The derived trust suggestion treats a **Manual** entry as measured and a Connector as
  estimated: typing a value already expresses a judgement, whereas a scale expresses only
  that a scale was stood on. Verve still asks rather than inferring from the r = 0.99
  correlation it can see (PRD, "does NOT do").
- `biological_sex` is validated with a message stating what it is for — an input to two of
  the four equations, nothing more — so the field does not read as Verve modelling gender.

**Remaining for issue 04:** the profile UI. It was always specified to live on the Plan
page, so it lands with the page rather than as a separate surface.
