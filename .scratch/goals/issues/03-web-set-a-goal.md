Status: ready-for-agent
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
