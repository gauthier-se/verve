# Workouts

- **Status:** Planned
- **Milestone:** M2
- **Related:** [Maps](maps.md), [User preferences and profile](user-preferences-and-profile.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Heart page](page-heart.md)

## Summary

A filterable list of workouts, a detailed view per workout with map and synchronized charts, heart rate zones, splits, personal records computed from routes, and weekly and monthly volume per sport.

## User stories

- As a runner, I want to see my route, pace and heart rate together, and where I slowed down.
- As a user, I want to know my best 5 km time even if I never ran exactly 5 km.
- As a user, I want to see how much I trained each week per sport.

## Scope

### In scope

- Workouts list, workout detail, heart rate zones, splits, personal records, training volume.

### Out of scope

- Training load (TRIMP, acute to chronic ratio, fitness and fatigue), workout comparison on the same route, GPX and FIT import (future).
- Editing workout data.

## Functional requirements

- **FR-1**: List with filters (activity type, date range, source, with route only, indoor or outdoor), columns (date, type, duration, distance, energy, average heart rate, elevation gain), sorting and pagination in a TanStack Table. Duplicates flagged by source deduplication are hidden by default.
- **FR-2**: Workout detail:
  - summary: type, start and end, duration, moving time when pauses exist, distance, active and total energy, elevation gain and loss, average and max heart rate, average pace or speed, temperature and humidity when available, source,
  - map with the route, colored by pace, heart rate or elevation (user choice),
  - synchronized charts of heart rate, pace or speed, elevation, and power or cadence when available: hovering a chart moves a marker on the map and on the other charts, and selecting a range highlights the segment on the map,
  - splits per km or per mile (time, pace, heart rate, elevation change),
  - laps and segments from workout events,
  - time in each heart rate zone.
- **FR-3**: Heart rate during a workout comes from heart rate samples within the workout time range, using the workout source first, then the metric priority.
- **FR-4**: Heart rate zones: 5 zones computed with the user's method:
  - percentage of maximum heart rate, default boundaries 50, 60, 70, 80, 90 percent,
  - Karvonen: percentage of heart rate reserve using resting heart rate,
  - custom boundaries in bpm.
  Time in zone is time-weighted between consecutive heart rate samples, ignoring gaps longer than 2 minutes.
- **FR-5**: Personal records per sport, recomputed in a background job after imports:
  - for running and walking: best efforts over 400 m, 1 km, 1 mile, 5 km, 10 km, half marathon and marathon, computed with a sliding window over the cumulative distance of every route (not only workouts of that exact length),
  - for all distance sports: longest distance, longest duration, largest elevation gain,
  - for cycling: best average speed over 10 km and 40 km.
- **FR-6**: Records keep history: each time a record improves, the previous value and workout stay visible. The workout detail shows badges for records set in that workout.
- **FR-7**: Volume: weekly and monthly totals per sport (count, duration, distance, elevation, energy), as stacked bars, with current week versus the average of the previous 4 weeks.
- **FR-8**: Indoor workouts without route show summary, charts and zones without map.

## UX notes

- Mobile detail: map on top (collapsible), then summary, then charts stacked with a shared scrubber.
- Activity type icons and localized names for all HealthKit activity types.

## Data and API notes

- `workouts`, `workout_routes`, `personal_records` tables.
- Endpoints: `GET /api/v1/workouts`, `GET /api/v1/workouts/{id}`, `GET /api/v1/workouts/{id}/streams` (downsampled time series and route), `GET /api/v1/records`, `GET /api/v1/workouts/volume`.
- Chart components: `TimeSeriesChart` with synchronized cursor, `ZoneBars`, `SplitsTable`.

## Edge cases

- GPS glitches (teleporting points): points implying speeds far above the sport's plausible maximum are excluded from distance and best efforts.
- Pool swimming: no route, distance from workout totals, splits from laps when available.
- Workouts crossing midnight: belong to the day of their start.
- Pace display for very slow segments (stops): capped to keep the chart readable.

## Acceptance criteria

- [ ] Hovering the heart rate chart moves the marker on the map at the right position.
- [ ] A 10 km run containing a fast 5 km produces a 5 km best effort.
- [ ] Zones reflect the selected method and update when settings change.
- [ ] A duplicated workout from a second app does not count twice in weekly volume.

## Future ideas

- Training load and fitness and fatigue curves.
- Compare two workouts on the same route.
- GPX and FIT import.
- Route privacy zones (see [Maps](maps.md)).
