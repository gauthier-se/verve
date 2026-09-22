Status: done
Blocked by: 02

# 03: web: Now at `/`, Dashboards at `/d`

## What

- **Routes** (`router.tsx`): `/` renders `NowPage`; `/d` renders the current
  `DashboardIndex` (forward to the first Dashboard, or the empty state). The
  "Dashboards" tab and the dashboard delete redirect point at `/d`. The logo
  keeps `/`.
- **`NowPage`**, one query on `/v1/now`:
  - **Freshness block**: "Last data: 12 Sept, 10 days ago", then the active
    Sources with their last day and lag ("Apple Watch · 3 weeks behind"), the
    leader without a lag. Retired Sources collapsed under "Retired sources (N)".
    Links to History, where the gaps are drawn. No colour on any age.
  - **Pin cards**: Metric label, Latest value formatted like the Day page,
    "12 Sept · 10 days ago" (or "today"), the bound with `boundText`, the
    Attainment with `attainmentText` ("5 of 6 measured days"). A card links to
    the Metric page; its date links to the Day page. A Pin never measured says
    so in words.
  - **No Pin**: the Freshness block and one line: pin a Metric from its page.
  - **No data**: the import invitation linking to `/import`.
- Ages are the server's `age_days`, never recomputed from a client date.

## Tests

- vitest on the age wording (0, 1, n days, weeks) and the retired fold;
- `/` renders Now, `/d` forwards to the first Dashboard;
- `tsc` and the production build pass.

## Comments

Shipped. `now-page.tsx`, `use-now.ts`, `lib/now.ts` with `now.test.ts`, `/` and `/d`
in `router.tsx`, a "Now" row above the Dashboards in the sidebar and a "Now" tab
before "Dashboards" in the tab bar.

What differs from the spec:

- **Ages are written in days up to 20, then weeks, months, years.** "2 weeks ago"
  for 14 to 20 days hides a week the reader would want to know about.
- **Every write that can move Now invalidates it**: a Pin, a Goal, a Manual entry,
  an Exclusion, and the end of an import (which now also stales the Ledger, which it
  did not before).
- **The import report's "View your dashboard" goes to `/d`**, since `/` is no longer
  a dashboard.
- Checked against a copy of a real history: `/v1/now` names the last datum
  (2026-07-25, 59 days old), 5 active and 8 retired Sources, and five Pin cards,
  in 37 ms once `Latest` read one day first (see the perf commit).

Not yet looked at in a browser: the pane needs a login.
