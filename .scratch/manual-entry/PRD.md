# PRD — Manual entry

## Goal

Let an Account write its own Measurements. Today the `measurements` table has exactly
one writer — a Connector running an idempotent import — so a Metric that is missing,
stale, or untrustworthy cannot be fixed. On the reference Account `height` has three
Measurements, the last from 2024-08-28; an Account with no smart scale has no
`body_mass` at all; and `body_fat_percentage` comes from a scale whose figure
correlates with body mass at r = 0.9895 over 474 days — a weight lookup wearing the
costume of a composition measurement.

A **Manual entry** is an ordinary Measurement carrying the reserved Source `Manual`,
ranked first in Source priority. It does not mask the device reading; it outranks it
in its own bucket.

This is a platform capability. The Plan milestone (`.scratch/energy-planning/`) is its
first consumer, not its owner.

Context, glossary, and the design decision: see `CONTEXT.md` (**Manual entry**) and
`docs/adr/0022`.

## What this milestone does

- **`POST /v1/measurements`** — creates a Measurement with Source `Manual`. Any
  Catalog slug, **canonical unit only** (no conversion, no unit choice), a value, and
  a date with optional time defaulting to now. Content key computed exactly as the
  import path does (ADR 0006), so submitting the same entry twice is idempotent
  rather than duplicative.
- **`DELETE /v1/measurements/{id}`** — **403 when the Source is not `Manual`**. The
  guard lives in the model's `WHERE` clause, not only in the handler. Correcting a
  value is delete-then-re-enter.
- **`GET /v1/measurements?source=manual`** — the manual entries an Account has
  written, most recent first, filterable by Metric. Needed because the Ledger serves
  aggregated Series rows (ADR 0021) which carry no measurement id, and you cannot
  delete what you cannot address.
- **Manual overlay** — on a day the Account has typed a value for a Metric, that
  day's Manual rows replace the winning Source's rows; every other day is untouched.
  Applied in the source predicate shared by every read path, and **skipped entirely**
  when the Metric has no Manual rows, so existing behaviour is unchanged for anyone
  who has never typed one. `Manual` does *not* enter Source priority: that mechanic
  elects one winner for the whole range, so ranking Manual first would collapse a
  905-point weight chart to the single day that was typed.
- **Web: a manual-entry surface** — Metric picker over the Catalog (searchable; the
  Catalog is ~80 entries), value field labelled with the canonical unit, date/time,
  submit. Below it, the Account's recent manual entries with a delete affordance.
- **Validation** — unknown slug → 422; a Metric whose family is not `Measurement`
  (State, Session) → 422 with a message saying so; non-finite or absurd values
  rejected per Metric bounds where the Catalog can express them.

## What this milestone does NOT do

- **Editing in place.** No `PATCH`. The content key is derived from the value, so an
  edit would have to recompute it and resolve collisions with an existing key —
  cases that buy one gesture and cost real complexity.
- **Deleting imported data.** Structurally refused, not merely unimplemented:
  removing an imported row drops its content key and the next Apple import restores
  the row verbatim. Making deletion stick needs a tombstone table consulted at
  import time — a separate feature.
- **Bulk entry, CSV paste, or backfilling a range.** One value, one date.
- **State, Session, or Meal entry.** Measurements only; the other families have
  shapes (intervals, sub-measurements, routes) that need their own UI.
- **A "log my breakfast" flow.** This is data correction, not food logging.

## Issues

Order: 01 → 04 → 02 → 03. Issue 04 is a **precondition for `Manual` rows being safe
to read at all**, and was added after the code showed that Source priority resolves
whole-range, not per bucket.

1. `01-manual-source-and-model` — data: reserved `Manual` Source, single-row insert,
   guarded `Delete`, `ListManual`.
2. `04-manual-overlay-query` — query engine: day-grain overlay in the shared source
   predicate, skipped when no Manual rows exist.
3. `02-measurements-api` — api: `POST` / `DELETE` / `GET` handlers, validation, the
   403 contract.
4. `03-web-manual-entry` — web: entry form, Catalog picker, recent-entries list with
   delete.
