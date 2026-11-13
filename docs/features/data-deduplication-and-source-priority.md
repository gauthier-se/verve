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

  - The pipeline computes the natural key of each sample as a hash and chooses its rule. The key of a sample with a uuid is derived from the uuid, so that one unique index on `samples (user_id, natural_key)` covers every sample. Samples are stored in batches, in order: a sample whose key appeared earlier in its batch meets that one as stored.
  - Update if the value changed compares the value, min, max and category value, and when one of them differs replaces all four and counts the sample as updated, otherwise as a duplicate. Replace replaces the value, min and max, and always counts the sample as updated. Neither changes the other fields of the stored sample, its start included.
  - Update, for a workout with an external id, compares every field of the stored workout but the import id. When one of them differs, it replaces every field but the id, so that the route of the workout stays attached, and counts the workout as updated. Otherwise it changes nothing, the import id included, and counts the workout as a duplicate, so that a source that sends its workouts again, as Health Auto Export does on each sync, rewrites nothing.
  - A batch with an invalid sample, such as a type that does not exist or a source of another user, is refused as a whole, and nothing of it is stored.

- **FR-2**: Sources are registered per user from the source name, bundle id and device information when available.
  - The identity of a source is `(user, name, device model)`. The name is compared as received. The device model is the `model` of the HealthKit device (for example `Watch` or `iPhone`), from the `device` string of the XML export or the model of the native format, and is empty when unknown, as in Health Auto Export payloads, which carry only a name.
  - A record with a device model matches the source of that identity. Otherwise it matches the source of that name without model, which then takes the model, and otherwise it creates a source.
  - A record without device model matches the only source of that name. When several sources have that name, it matches the one without model. Otherwise it creates a source without model.
  - Bundle id, device name, manufacturer, and hardware and software versions are recorded on the source, but are not part of its identity. The aliases of merged sources (FR-3) are matched by the same identity: a record whose identity is an alias lands in the source of the alias, and an alias without model takes the model as a source does. No identity is both an alias and a source of the same user.
  - Each source records when it was first and last seen, from the time the pipeline gives for the records of the source: the latest start of its records in a batch, since the pipeline resolves each source once per batch. First seen never moves, and last seen never moves back. A record not older than last seen replaces the details of the source with those it carries, and an older record only fills the details that the source lacks. A record without a detail, such as the name only payloads of Health Auto Export, never clears it.
  - Each source gets a detected kind: `watch`, `phone`, `scale`, `ring`, `app`, `manual`, `aggregated`, `unknown`. Adapters give `aggregated` to their synthetic sources. Otherwise the first rule that matches the source of a record gives its kind:
    1. A known smart scale or smart ring vendor, from a table in code of the bundle ids and source names of its apps: `scale` or `ring`. Bundle ids match exactly, names whatever their case. A scale reading written through the phone app of its vendor is thus `scale`.
    2. The Health app, the source of the entries made by hand: `manual`. It is known by its bundle id `com.apple.Health`, matched exactly, or, as in XML exports that carry no bundle id, by the name `Health`, whatever its case, with the device model `iPhone`. The `HKWasUserEntered` metadata is not used, since it describes a sample, not its source, and third-party apps set it too.
    3. The device model, from the XML `device` string (model field) or the native format: `watch` for `Watch`, `phone` for `iPhone`.
    4. A bundle id outside `com.apple.`, as written by a third-party app such as a running or nutrition app: `app`.
    5. Without device model, as in Health Auto Export payloads, a name that contains `Apple Watch` or `iPhone`, whatever its case, such as "Camille's Apple Watch": `watch` or `phone`. It comes after the bundle id, which is the stronger evidence.
    6. Otherwise `unknown`. A third-party app known only by its name, as in XML exports and Health Auto Export payloads, is `unknown`: no table of app names is kept, and FR-4 ranks `unknown` as an app.

    The kind can be corrected by the user. The detected kind follows the records as the details do, `unknown` counting as a missing kind. A kind corrected by the user is kept.
- **FR-3**: Users can rename the display name of a source and merge sources (for example the same Apple Watch recorded under an old and a new device name). Merging is an alias: future data from either name lands in the merged source, and aggregates are recomputed.
- **FR-4**: Each metric type has an ordered priority list of sources per user, plus an "ignored" section. Default ordering when no user choice exists:
  - activity and energy metrics: `aggregated`, then `watch`, then `ring`, then `phone`, then other apps, then `manual`,
  - body measurements: `scale`, then apps, then `manual`,
  - nutrition: apps in order of first appearance, then `manual`,
  - other metrics: `aggregated`, then `watch`, then `ring`, then `phone`, then others.

  Apps are the sources of kind `app` or `unknown`, since a third-party app known only by its name is `unknown` (FR-2).
- **FR-5**: New sources are appended at the end of the default position for their kind, and the user is notified in the UI ("New source detected: ...").
- **FR-6**: Aggregation selects, for each user, metric and hourly bucket, the highest priority non-ignored source that has data in that bucket. Only that source's samples contribute to the bucket.
- **FR-7**: For discrete metrics, the user can switch a metric to "combine sources" mode, where all non-ignored sources contribute (useful when two scales are used alternately).
- **FR-8**: Changing priorities, ignoring a source or merging sources triggers recomputation of the affected aggregates.
- **FR-9**: Raw sample views (metric explorer table, workout detail) always show the source of each sample and whether it is used in aggregates.
- **FR-10**: Sleep: for each night, sleep stages come from the highest priority source with sleep data for that night (see [Sleep page](page-sleep.md)).
- **FR-11**: Workouts: two workouts from different sources or different origins with the same activity family (the family of their activity type in the activity type catalog, see [Storage and aggregates](data-storage-and-aggregates.md) FR-4) overlapping by more than 80 percent of the shorter one are flagged as duplicates. The lower priority one is hidden from lists and totals but kept, and can be shown or promoted. Between two workouts of the same source, for example received through both an XML import and Health Auto Export, the one with a route is kept, then the one imported first.

## UX notes

- "Sources" settings page: list of sources with kind, sample count, first and last seen, rename and merge actions.
- Per-metric priority editor: drag and drop list, accessible from the metric explorer ("Sources used") and from settings.
- The metric explorer can show a stacked breakdown by source with the selected source highlighted per bucket.

## Data and API notes

- Tables: `sources` (unique `(user_id, name, device_model)`, the model being an empty string when unknown), `source_aliases`, `source_priorities` (user, type, ordered list, ignored list, combine flag).
- Endpoints: `GET/PATCH /api/v1/sources`, `POST /api/v1/sources/merge`, `GET/PUT /api/v1/metrics/{type}/sources`.

## Edge cases

- Watch not worn for part of an hour while the phone counted steps: the hour uses the watch only, so a few steps can be missed. This is the accepted trade-off of hour granularity, documented in the UI help.
- Summarized Health Auto Export data and raw XML data for the same hours: the `aggregated` source wins by default, no double counting.
- A source that changes kind (phone app moved to watch): user correction persists.
- Sources with identical names on two different devices: separated by device model (FR-2). A record of that name without model goes to the source of that name without model, or creates it.

## Acceptance criteria

- [ ] Daily steps computed from an export containing both iPhone and Apple Watch samples are within 2 percent of the Health app's daily total for the same days.
- [ ] Moving a source up in priority changes the aggregates after recomputation.
- [ ] Merging two sources makes their samples appear as one source everywhere.
- [ ] A Strava copy of an Apple Watch run is flagged as a duplicate and excluded from weekly volume.

## Future ideas

- Finer arbitration at 5 or 15 minute granularity, closer to the Health app's interval-based logic.
