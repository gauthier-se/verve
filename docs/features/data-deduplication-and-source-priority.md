# Deduplication and source priority

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Storage and aggregates](data-storage-and-aggregates.md), [Health Auto Export endpoint](ingestion-health-auto-export.md), [Sleep page](page-sleep.md), [Workouts](page-workouts.md)

## Summary

Two problems make naive sums wrong: the same data arrives several times (re-imports, overlapping syncs), and several devices record the same activity (iPhone and Apple Watch both count steps). Verve makes every import idempotent and, when aggregating, selects one source per metric and per hour according to a priority list, as the Health app does.

## User stories

- As a user, I want re-imports and overlapping syncs to never create duplicates.
- As a user wearing an Apple Watch, I want my step count to match the Health app, not twice that.
- As a user, I want to choose which device to trust for each metric.

## Scope

### In scope

- Natural keys and upsert rules per record origin.
- Source registry, source kind detection, merging renamed sources.
- Per-metric source priority with sensible defaults.
- Per-bucket source selection for aggregates.
- Duplicate workouts and sleep sessions across sources.

### Out of scope

- Sub-hour source arbitration (future).

## Functional requirements

- **FR-1**: Natural keys:

  | Record | Key | On conflict |
  |---|---|---|
  | Sample with uuid (native) | `(user, uuid)` | Ignore, or update if the value changed |
  | Raw sample without uuid (XML, unsummarized Health Auto Export) | `(user, type, source, start, end, value, category value)` | Ignore (duplicate) |
  | Pre-aggregated point (summarized Health Auto Export) | `(user, type, source, start, end)` | Replace value, min and max |
  | Workout with external id | `(user, origin, external id)` | Update |
  | Workout without external id (XML) | `(user, activity type, source, start, end)` | Ignore |

- **FR-2**: Sources are registered per user from the source name, bundle id and device information when available. Each source gets a detected kind: `watch`, `phone`, `scale`, `ring`, `app`, `aggregated`, `unknown`. Detection uses the XML `device` string (model field), known bundle ids and name patterns, and can be corrected by the user.
- **FR-3**: Users can rename the display name of a source and merge sources (for example the same Apple Watch recorded under an old and a new device name). Merging is an alias: future data from either name lands in the merged source, and aggregates are recomputed.
- **FR-4**: Each metric type has an ordered priority list of sources per user, plus an "ignored" section. Default ordering when no user choice exists:
  - activity and energy metrics: `aggregated`, then `watch`, then `ring`, then `phone`, then other apps,
  - body measurements: `scale`, then apps, then manual entries in the Health app,
  - nutrition: apps in order of first appearance,
  - other metrics: `aggregated`, then `watch`, then `ring`, then `phone`, then others.
- **FR-5**: New sources are appended at the end of the default position for their kind, and the user is notified in the UI ("New source detected: ...").
- **FR-6**: Aggregation selects, for each user, metric and hourly bucket, the highest priority non-ignored source that has data in that bucket. Only that source's samples contribute to the bucket.
- **FR-7**: For discrete metrics, the user can switch a metric to "combine sources" mode, where all non-ignored sources contribute (useful when two scales are used alternately).
- **FR-8**: Changing priorities, ignoring a source or merging sources triggers recomputation of the affected aggregates.
- **FR-9**: Raw sample views (metric explorer table, workout detail) always show the source of each sample and whether it is used in aggregates.
- **FR-10**: Sleep: for each night, sleep stages come from the highest priority source with sleep data for that night (see [Sleep page](page-sleep.md)).
- **FR-11**: Workouts: two workouts from different sources or different origins with the same activity family overlapping by more than 80 percent of the shorter one are flagged as duplicates. The lower priority one is hidden from lists and totals but kept, and can be shown or promoted. Between two workouts of the same source, for example received through both an XML import and Health Auto Export, the one with a route is kept, then the one imported first.

## UX notes

- "Sources" settings page: list of sources with kind, sample count, first and last seen, rename and merge actions.
- Per-metric priority editor: drag and drop list, accessible from the metric explorer ("Sources used") and from settings.
- The metric explorer can show a stacked breakdown by source with the selected source highlighted per bucket.

## Data and API notes

- Tables: `sources`, `source_aliases`, `source_priorities` (user, type, ordered list, ignored list, combine flag).
- Endpoints: `GET/PATCH /api/v1/sources`, `POST /api/v1/sources/merge`, `GET/PUT /api/v1/metrics/{type}/sources`.

## Edge cases

- Watch not worn for part of an hour while the phone counted steps: the hour uses the watch only, so a few steps can be missed. This is the accepted trade-off of hour granularity, documented in the UI help.
- Summarized Health Auto Export data and raw XML data for the same hours: the `aggregated` source wins by default, no double counting.
- A source that changes kind (phone app moved to watch): user correction persists.
- Sources with identical names on two different devices: separated by device model when available, otherwise treated as one source.

## Acceptance criteria

- [ ] Daily steps computed from an export containing both iPhone and Apple Watch samples are within 2 percent of the Health app's daily total for the same days.
- [ ] Moving a source up in priority changes the aggregates after recomputation.
- [ ] Merging two sources makes their samples appear as one source everywhere.
- [ ] A Strava copy of an Apple Watch run is flagged as a duplicate and excluded from weekly volume.

## Future ideas

- Finer arbitration at 5 or 15 minute granularity, closer to the Health app's interval-based logic.
