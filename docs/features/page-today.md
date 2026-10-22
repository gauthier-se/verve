# Today page

- **Status:** Planned
- **Milestone:** M2
- **Related:** [Metric explorer](page-metric-explorer.md), [Sleep page](page-sleep.md), [Workouts](page-workouts.md), [Plans and check-ins](goals-plans-and-check-ins.md), [Health Auto Export endpoint](ingestion-health-auto-export.md)

## Summary

The home page: a glanceable overview of the current day with the key metrics, each compared to recent averages, plus last night's sleep, the latest workout, plan progress and sync freshness.

## User stories

- As a user, I want to see at a glance how my day compares to my usual days.
- As a user, I want to know immediately if my data is stale.
- As a user on a plan, I want to see today's calories and macros against my targets.

## Scope

### In scope

- Metric cards with trends, sleep card, last workout card, plan card, sync status.
- Day navigation.
- Hiding and ordering cards.

### Out of scope

- Free-form widgets (see [Custom dashboards](custom-dashboards.md)).
- Computed "readiness" or "recovery" scores.

## Functional requirements

- **FR-1**: Default cards, shown only when data exists:

  | Card | Content |
  |---|---|
  | Steps | Total, progress to goal, hourly bars |
  | Active energy | Total, hourly bars |
  | Exercise minutes | Total, weekly progress to goal |
  | Resting heart rate | Latest value |
  | Heart rate variability | Latest value (SDNN) |
  | Sleep | Last night: total sleep, time in bed, stage bar, bedtime and wake time |
  | Weight | Latest weigh-in and trend weight |
  | Nutrition | Calories and macros consumed, against plan targets when a plan is active |
  | Last workout | Type, date, duration, distance, map thumbnail when a route exists |
  | Plan | Days into the plan, trend weight versus trajectory, next check-in date |

- **FR-2**: Each metric card shows the delta against the 7-day and 30-day averages, a 7-day sparkline, and links to the metric explorer with the right period.
- **FR-3**: For cumulative metrics on the current day, the comparison is made against the average at the same time of day, so that a morning is not compared to full days.
- **FR-4**: Day navigation (previous, next, date picker) shows any past day with the same layout.
- **FR-5**: A sync status line shows the last data received per origin, in warning color when older than the user's stale threshold (default 24 hours), with a link to the import history.
- **FR-6**: Users can hide cards and reorder them. The layout is stored server-side.
- **FR-7**: The page refreshes its data when the app regains focus and every 5 minutes while visible.

## UX notes

- Mobile: single column, cards ordered by the user's ordering, sleep and plan cards near the top by default.
- Desktop: responsive grid.
- Empty state for new users: guidance to import data, link to the Health Auto Export wizard.

## Data and API notes

- `GET /api/v1/today?date=YYYY-MM-DD` returns all card data in one response to minimize requests.
- Card layout stored with user preferences.

## Edge cases

- Day with no data at all (watch not worn): cards show "No data" rather than zeros.
- Sleep crossing the day boundary: assigned by the sleep window, see [Sleep page](page-sleep.md).
- Latest weigh-in several days old: shown with its date.

## Acceptance criteria

- [ ] The page loads with a single API call.
- [ ] Today's partial cumulative values are compared to the same time of day.
- [ ] Stale sync is visible without opening settings.

## Future ideas

- Activity rings when activity summaries are supported.
