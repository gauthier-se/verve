Status: done
Blocked by: 01

# 03: web: setting a Goal on the Metric page

## What

- **A "Goal" section on the Metric page** (`web/src/components/metric-page.tsx`),
  fed by a `hooks/use-goals.ts` over `GET /v1/goals?metric=`.
  - The Goal in force, written as a fact: "at least 7 500 steps a day, since
    12 March".
  - The history below it: each entry with its direction, value and dates.
  - Three actions: set (which closes the current one), close, delete an entry.
- **The form**: direction, value, start date (today by default, editable back in
  time, never forward). The value is entered in the Metric's canonical unit,
  except `sleep`, entered as hours and minutes and stored in minutes
  (`formatDuration`'s inverse). A server refusal on the start date is shown as
  the server wrote it.
- **Not shown for a `latest` Metric**: no section, no disabled form. The page
  already knows the aggregation from the Catalog.
- **Nothing in the Panel editor.** A Goal is the Metric's and the Account's.

## Tests

- setting a Goal invalidates both the goals query and every series query for
  that Metric, so an open Panel redraws;
- the sleep form round-trips 7 h 30 as 450;
- the section is absent on `body_mass`.

## Why here

The Metric page is already where a Metric's own facts live (its Sources, its
Ledger, its Pin). A Goal is one more, and putting the form there says where it
belongs.

## Comments

Shipped. `components/goal-card.tsx` under the window stats on the Metric page,
`hooks/use-goals.ts`, and `lib/goals.ts` with `goals.test.ts`.

What differs from the spec:

- **The percent rule moved to `lib/metrics.ts`** (`toStoredValue`,
  `toDisplayValue`). A Goal on `oxygen_saturation` is typed as 95 and stored as
  0.95, exactly as a Manual entry is, and the Manual entry's private copy of the
  rule said, in its own comment, that a second copy is how a 26-point error gets
  shipped. `manual-entry-dialog.tsx` now imports it.
- **The start date defaults to today on the UTC clock** (`todayUTC`), not the
  browser's, because the server refuses a future date on its own clock and an
  evening in UTC-5 is already tomorrow there.
- **"Stop" is hidden on the day a Goal starts**, since the server refuses a close
  on the start date. Delete stays available.
- **No hook tests.** The suite covers pure modules only (`vite.config.ts` says
  why), so the invalidation list is not under test. It invalidates `goals`,
  `series` and `day`, the Day being where issue 05 prints the bound.

Checked end to end against a throwaway instance with synthetic Manual steps,
through the API: a 7-day window read 7 covered, 6 measured, 2 met; a 30-day one
at week grain read 20 covered (from the Goal's start), 8 measured, 4 met;
`body_mass` was refused. The page itself was not driven in a browser.
