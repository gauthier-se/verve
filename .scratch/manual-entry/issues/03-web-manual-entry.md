# 03 — Web: the manual-entry surface

Status: done
Blocked by: 02

## Goal

Give the Account a place to type a Measurement and to remove one it typed — the
first write path in Verve's UI that is not a file upload.

## Scope

- **Entry form**, reachable from the Data page (it belongs with the numbers, not with
  the dashboards):
  - **Metric picker** — searchable combobox over the Catalog. ~80 entries, so a plain
    `<select>` is not enough. Exclude derived Metrics and non-Measurement families;
    the API rejects them anyway, but they should not be offered.
  - **Value field** — labelled with the canonical unit, taken from the Catalog.
  - **Date + optional time** — defaults to now. Reuse `day-range-picker.tsx`'s date
    control if it factors cleanly; do not fork a second date widget.
  - Submit → optimistic invalidate of the affected Series and Ledger queries.
- **Percent Metrics need a display scale.** The store keeps fractions (`0.27` for
  `body_fat_percentage`), which nobody will type. For a `%` Metric, present the field
  in 0–100 and convert on submit, with the field clearly suffixed `%`. Do this in one
  place, keyed off the Catalog unit, not per Metric.
- **Recent manual entries** — a list below the form from
  `GET /v1/measurements?source=manual`: Metric, value, date, delete button with a
  confirm. Empty state explains what a manual entry is for.
- **Types** — extend `web/src/lib/types.ts` with the measurement shape and mirror the
  API contract exactly.
- **Hook** — `web/src/hooks/use-measurements.ts` alongside the existing
  `use-dashboards` / `use-auth` hooks; mutations invalidate `series` and `ledger`.

## Out of scope

- Editing an entry. Delete then re-enter (ADR 0022).
- Entry from a Panel or a chart point.
- Bulk / CSV import — that is the Connector path.
- The profile fields (age, sex, body-composition trust) — those live on the Plan page
  and ship with `.scratch/energy-planning/`.

## Acceptance

- A Metric can be picked, a value typed, and the entry appears in the recent list
  without a page reload.
- Entering `22` for `body_fat_percentage` stores `0.22`, and the recent list shows it
  back as `22 %` — the round-trip does not drift.
- Deleting an entry removes it from the list and the underlying Series reverts to the
  device value for that bucket.
- A Metric already covered by a device on the same day shows the manual value in the
  Ledger afterwards.
- The Catalog picker offers no derived Metric and no sleep Metric.
- Keyboard-navigable; the form works without a mouse.

## Refs

CONTEXT.md: **Manual entry**, **Catalog**. ADR 0022, ADR 0013 (client SPA).
`web/src/components/data-page.tsx` (host), `web/src/lib/metrics.ts` (Catalog
metadata on the client), `web/src/components/ui/` (shadcn primitives),
`web/src/components/day-range-picker.tsx` (date control to reuse).

## Comments

Implemented on branch `feat/manual-entry`.

- `web/src/components/manual-entry-dialog.tsx` — searchable Catalog picker (same shape as
  `add-panel-dialog.tsx`, deliberately), value field labelled with the canonical unit,
  `datetime-local` defaulting to now, and the entries list with delete underneath. Hosted
  from the Data page header ("Enter a value"), where the numbers already live.
- `web/src/hooks/use-measurements.ts` — list/create/delete, following the
  `useInvalidate` shape of `use-dashboards.ts`.
- **Invalidation covers `series` and `ledger`, not just the entry list.** A Manual entry
  changes the resolved row set behind the graphs (the overlay), so refreshing only the
  list would leave the charts displaying the very value that was just corrected.
- **The percent rescale lives in exactly one place**, keyed off the Catalog unit:
  `toStored` / `toDisplay` in the dialog. A second copy of that rule is how a 26-point
  error that still looks plausible gets shipped.
- The picker excludes derived Metrics (computed, never entered — ADR 0014) and
  `duration_by_state`; offering either would be an invitation to a 422.
- `tsc --noEmit` and `vite build` clean. No lint script exists in this project.

### Deviations from the issue

- **The date control was not reused.** `day-range-picker.tsx` is a range picker built on
  the calendar popover; a single instant with an optional time is a different control,
  and bending it would have cost more than the native `datetime-local` it replaced.
  Noted rather than silently skipped.
- **Reached from the Data page header rather than a section below the form.** Same page,
  one dialog holding both the form and the deletable list, which keeps entry and undo in
  the same glance.

### End-to-end verification

Against the real binary on a throwaway DB, not fixtures: register → POST an entry (201)
→ replay it (200, idempotent) → derived Metric refused (422 with its message) → the
series shows 0.27 / **0.22** / 0.27 across three days with `source` still "Zepp Life" →
DELETE (204) → the series reverts to 0.27 throughout and `mean` moves 0.2533 → 0.27,
confirming `summaryMean` goes through the shared filter too.
