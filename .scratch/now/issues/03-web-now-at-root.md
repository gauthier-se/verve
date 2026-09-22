Status: ready-for-agent
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
