# Custom dashboards

- **Status:** Planned
- **Milestone:** M4
- **Related:** [Metric explorer](page-metric-explorer.md), [Plans and check-ins](goals-plans-and-check-ins.md), [ADR 0003](../decisions/0003-tanstack-charts-behind-wrappers.md)

## Summary

Users build their own dashboards from a grid of configurable widgets: values, time series, goal progress, plan progress, last workout or night, calendar heatmaps. Each user can have several dashboards. Fixed pages (Today, Sleep, Heart...) stay; custom dashboards come in addition.

## User stories

- As a runner, I want a "Running" dashboard with weekly distance, VO2max and last run.
- As a user on a cut, I want weight trend and calories consumed on the same chart.
- As a user, I want the same dashboard on my phone and my computer.

## Scope

### In scope

- Multiple dashboards per user, default dashboard.
- Drag and drop grid, responsive layouts.
- Widget catalog and configuration.
- Templates to start from.

### Out of scope

- Sharing dashboards with other users, JSON import and export (future).
- Arbitrary formulas between metrics.

## Functional requirements

- **FR-1**: A user can create, rename, duplicate, reorder and delete dashboards, and mark one as default.
- **FR-2**: Grid layout with draggable and resizable widgets (react-grid-layout), with layouts per breakpoint. On small screens, widgets stack in a single column following the desktop order unless the user customizes the mobile layout.
- **FR-3**: View mode and edit mode. Edit mode shows handles, widget settings and the add widget panel.
- **FR-4**: Widget types:

  | Widget | Configuration |
  |---|---|
  | Single value | Metric, statistic (sum, mean, min, max, last), period, comparison with previous period |
  | Time series | One or two metrics (second on its own axis), period, bucket, chart style, optional goal line, optional moving average |
  | Goal progress | Metric, goal value, period (day, week, month), ring or bar |
  | Plan progress | Active plan trajectory or today's targets versus consumed |
  | Last workout | Activity type filter, with map thumbnail |
  | Last night | Hypnogram and statistics |
  | Calendar heatmap | Metric, statistic, one year by default |
  | Personal records | Sport filter |

- **FR-5**: Each dashboard has a global period (for example 7 days, 30 days, 1 year). Widgets use it unless they override it.
- **FR-6**: Templates when creating a dashboard: Overview, Running, Cut or bulk, Sleep and recovery, Blank.
- **FR-7**: A dashboard has at most 30 widgets.
- **FR-8**: Widget data is fetched with one batched request per dashboard load to limit round trips.
- **FR-9**: Widgets referencing metrics without data show an empty state, not an error.

## UX notes

- Add widget panel with live preview.
- Widgets reuse the same chart wrappers as fixed pages, so visuals are consistent.

## Data and API notes

- `dashboards` table: user, name, position, default flag, global period, widgets (JSON with type and configuration), layouts per breakpoint (JSON).
- Endpoints: `GET/POST /api/v1/dashboards`, `GET/PUT/DELETE /api/v1/dashboards/{id}`, `POST /api/v1/dashboards/{id}/data` (batched widget data).
- Widget configuration is versioned so it can evolve with migrations.

## Edge cases

- Metric deleted or blocked after a widget was created: widget shows "No data" with a hint.
- Concurrent edits from two devices: last save wins, with a warning when the dashboard changed since it was loaded.

## Acceptance criteria

- [ ] A dashboard edited on desktop appears with the same widgets on mobile, stacked.
- [ ] A dashboard with 30 widgets loads with a single data request.
- [ ] Templates create working dashboards for a user with typical Apple Watch data.

## Future ideas

- JSON import and export of dashboards to share configurations with the community.
- Sharing dashboards with other accounts.
