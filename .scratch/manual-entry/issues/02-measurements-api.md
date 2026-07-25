# 02 — API: create, list and delete manual Measurements

Status: ready-for-agent
Blocked by: 01

## Goal

Expose the three endpoints that let an Account write and unwrite its own
Measurements, with the 403-on-non-Manual contract visible in the API and not only in
the database (ADR 0022).

## Scope

- **`POST /v1/measurements`** (`requireAuth`), body:
  ```json
  { "metric": "body_fat_percentage", "value": 0.22, "measuredAt": "2026-07-25T08:30:00Z" }
  ```
  - `metric` must exist in the Catalog → else 422.
  - The Metric must be of the **Measurement** family and **imported**, not derived →
    else 422 with a message naming the reason (a derived Metric is computed, not
    entered).
  - `value` finite; the **canonical unit is implied and never sent** — the response
    echoes it so the client can label the field.
  - `measuredAt` optional, defaults to now; a future timestamp beyond a small skew
    → 422.
  - Returns **201** with the row and its id; an idempotent re-submit returns **200**
    with the existing id (the model already resolves this — do not turn it into a
    409).
  - **Beware the unit scale.** `%` Metrics are stored as **fractions**:
    `body_fat_percentage` is `0.27`, not `27`, and `oxygen_saturation` is `0.969`.
    The API takes the canonical stored value; presenting a friendlier scale is the
    client's problem (issue 03) and must not be papered over here.
- **`GET /v1/measurements?source=manual&metric=<slug>&limit=<n>`** — the Account's
  manual entries, newest first. `source=manual` is currently the only accepted value;
  reject anything else with 422 rather than silently listing everything.
- **`DELETE /v1/measurements/{id}`** — read the row first to answer precisely:
  - absent, or owned by another Account → **404** (never leak existence)
  - present, owned, Source ≠ `Manual` → **403**, message: only manual entries can be
    deleted
  - otherwise → **204**
- Route registration in `internal/api/server.go` beside the existing `/v1/ledger` and
  `/v1/imports` entries, all behind `requireAuth`.

## Out of scope

- Bulk create. One measurement per request.
- `PATCH`. There is none (ADR 0022).
- Any Plan-specific endpoint (see `.scratch/energy-planning/`).

## Acceptance

- Creating, listing and deleting a manual entry round-trips end to end.
- `POST` with an unknown slug, a derived slug (`calorie_balance`), or a
  `duration_by_state` Metric each yield 422 with distinguishable messages.
- Posting the same payload twice yields one row: 201 then 200, same id.
- `DELETE` on an imported row returns **403** and the row survives — the test asserts
  both the status *and* that a subsequent read still finds it.
- `DELETE` on another Account's manual row returns 404 and the row survives.
- A manual `height` posted today is what `GET /v1/series?metric=height` returns for
  today, over the 2024 device value.

## Refs

ADR 0022, ADR 0002 (closed Catalog), ADR 0003.
`internal/api/server.go`, `internal/api/handlers.go`, `internal/api/validator.go`,
`internal/api/errors.go`. Follow the shape of `ledgerhandlers.go` for a small
handler + its test file.
