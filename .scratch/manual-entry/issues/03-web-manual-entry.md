# 03 — Web: the manual-entry surface

Status: ready-for-agent
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
