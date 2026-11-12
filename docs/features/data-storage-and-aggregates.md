# Storage and aggregates

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Metric explorer](page-metric-explorer.md), [ADR 0001](../decisions/0001-sqlite-behind-storage-interface.md), [ADR 0008](../decisions/0008-generic-sample-storage.md), [ADR 0012](../decisions/0012-local-hour-aggregates.md), [ADR 0014](../decisions/0014-route-encoding.md)

## Summary

All quantity and category samples are stored generically, workouts and routes have dedicated storage, and hourly and daily aggregates are precomputed so that dashboards stay fast over years of data. A metric type catalog describes each type: kind, canonical unit, aggregation style and display information.

## User stories

- As a user, I want every metric Apple Health recorded to be kept, even if Verve has no chart for it yet.
- As a user, I want a one-year chart to load instantly.
- As a contributor, I want to add a chart for a new metric without a migration or a re-import.

## Scope

### In scope

- Metric type catalog.
- Samples, workouts and routes storage.
- Hourly and daily aggregates, incremental recomputation.
- Series read API with server-side downsampling.

### Out of scope

- Source selection rules (see [Deduplication and source priority](data-deduplication-and-source-priority.md)).
- Postgres implementation.

## Functional requirements

- **FR-1**: The catalog ships with the binary and lists known HealthKit quantity and category types with: key (for example `heart_rate`), HealthKit identifier, kind (`quantity` or `category`), canonical unit, aggregation style (`cumulative` such as steps and energy, or `discrete` such as heart rate and weight), display group (Activity, Heart, Body, Sleep, Nutrition, Respiratory, Vitals, Mobility, Hearing, Cycle, Symptoms, Other), and for categories the keys of their values (for example `asleep_core` for `HKCategoryValueSleepAnalysisAsleepCore`).
  - Names, descriptions and category values are not stored in the catalog. The API returns the metric key and the value keys, and the web app translates them in the i18next namespace `metrics` (`web/src/locales/<language>/metrics.json`), keyed by the metric key: `<key>.name`, `<key>.description` and `<key>.values.<value>`. An uncurated type (FR-2) has no translation and is shown by its identifier, and its category values as received.
  - The catalog also lists the identifiers that are known but unsupported, for data that Verve does not store yet. The pipeline counts their records as unsupported ([Ingestion pipeline](ingestion-pipeline.md) FR-3) and never creates an uncurated type for them:

    | Data | Identifiers |
    |---|---|
    | Electrocardiograms | `HKDataTypeIdentifierElectrocardiogram` |
    | Audiograms | `HKDataTypeIdentifierAudiogram` |
    | State of mind | `HKDataTypeIdentifierStateOfMind` |
    | Medications | `HKMedicationDoseEventTypeIdentifierMedicationDoseEvent` |
    | Clinical records | every identifier starting with `HKClinicalTypeIdentifier` |

    Data that does not arrive as records of a type identifier, such as the activity summaries and vision prescriptions of the XML export or the `ecg`, `medications` and `stateOfMind` sections of Health Auto Export, is counted as unsupported by its adapter.

  - At each start, after the migrations and before the job worker, the types of the catalog are synchronized into `metric_types` in one transaction, matched by identifier. A missing type is created, and a stored one takes the attributes of the catalog, keeping its id since samples reference it. An uncurated type (FR-2) that the catalog now lists becomes curated. A curated type that the catalog no longer lists becomes uncurated and keeps its attributes, its display group included. A type that the catalog does not list and whose key the catalog gives to another type gets a new key by the rule of FR-2.

- **FR-2**: Types not in the catalog are created on first sight as "uncurated" with the received identifier and unit, kind inferred from the payload and a default aggregation style of `discrete`. They appear in the metric explorer under "Other".
  - The catalog is instance-wide: a type created on first sight is shared by every user, and holds no health data. An uncurated type is listed to a user only once they have samples of it, so that the apps of one user never show up in the list of another.
  - The key of an uncurated type is derived from its identifier: the prefix `HKQuantityTypeIdentifier` or `HKCategoryTypeIdentifier` is stripped, and the rest is converted to snake case (`HKQuantityTypeIdentifierUVExposure` gives `uv_exposure`). Any other identifier, such as a Health Auto Export name, is converted to snake case in full. Snake case splits words where a lower case letter or a digit is followed by an upper case letter, and before the last upper case letter of a run that is followed by a lower case letter, replaces each run of characters other than ASCII letters and digits with one underscore, trims underscores and lowercases the result (`VO2Max` gives `vo2_max`, `Blood Oxygen` gives `blood_oxygen`). A key already taken by another type gets the suffix `_2`, then `_3` and so on. An identifier that leaves an empty key is keyed `uncurated`, with the same suffixes.
- **FR-3**: A sample stores: user, type, source, start (UTC), end (UTC), original UTC offset, numeric value in canonical unit, optional min and max (pre-aggregated points), optional category value, origin, import id, optional external uuid, natural key hash and optional metadata (JSON).
  - The category value is the key of a value listed by the type in the catalog (FR-1). A sample of an uncurated category type keeps the value as received.
  - The import id references no table: it is kept for diagnosis, and purging import reports ([Ingestion pipeline](ingestion-pipeline.md) FR-11) never touches samples.
- **FR-4**: A workout stores: user, source, external id, activity type, start, end, duration, distance, active energy, total energy, elevation gain and loss, average and max heart rate, indoor flag, location type, events (pauses, laps, segments), metadata.
  - The activity type is a key from a catalog of activity types shipped with the binary (for example `running` for `HKWorkoutActivityTypeRunning`). Each activity type has a family from a closed list, which [Deduplication and source priority](data-deduplication-and-source-priority.md) FR-11 compares: `running`, `walking`, `hiking`, `cycling`, `swimming`, `rowing`, `strength`, `cardio`, `dance`, `mind_and_body`, `team_sport`, `racket_sport`, `winter_sport`, `water_sport`, `combat`, `other`. The list of families changes only with the code.
  - The activity type catalog (`internal/catalog/activities.yaml`) lists every value of `HKWorkoutActivityType` with its key, its name in the XML export, its HealthKit raw value and its family, and finds an activity type by any of the first three. Names are not stored in the catalog: the web app translates the key in the i18next namespace `activities` (`web/src/locales/<language>/activities.json`), keyed by the activity key.
- **FR-5**: A route is stored once per workout as a compressed, columnar encoded blob (time, latitude, longitude, altitude, speed, course, horizontal and vertical accuracies), with its bounding box, total distance and a simplified polyline for list thumbnails. The encoding is defined in [ADR 0014](../decisions/0014-route-encoding.md): a route round-trips through it exactly at the precisions of that record.
- **FR-6**: Hourly buckets are aligned to the hours of the user's timezone (identical to UTC hours for whole-hour offsets). Daily buckets are the user's local days. Weekly, monthly and yearly values are computed on read from daily aggregates, using the user's week start.
- **FR-7**: Each aggregate row stores, after source selection: sum, count, min, max, mean, last value and its time, the chosen source (or "combined"), and for daily rows the number of hours with data.
- **FR-8**: Cumulative samples spanning several buckets are prorated by overlap duration. Discrete samples are assigned to the bucket containing their start.
- **FR-9**: The pipeline records dirty ranges per user and type. A recompute job merges overlapping ranges and recomputes the affected hours, then the affected days.
- **FR-10**: Full recomputation for a user and type is triggered by: a change of source priority for that type, creation or removal of a block rule affecting it, a deletion, a change of the user's timezone (all types).
- **FR-11**: Series read API: given type, period, bucket (`hour`, `day`, `week`, `month`, `year`, or `auto`) and statistic (`sum`, `mean`, `min`, `max`, `last`), returns points from aggregates. `auto` picks a bucket that yields at most about 400 points.
- **FR-12**: Raw samples API for short ranges, paginated, with an optional `maxPoints` parameter that downsamples with a shape-preserving algorithm (largest triangle three buckets) for raw line charts.
- **FR-13**: Charts and widgets read aggregates by default. Raw reads are limited to a configurable maximum range (for example 7 days).

## UX notes

- While aggregates are being recomputed after an import, affected charts show a small "updating" indicator rather than stale numbers without warning.

## Data and API notes

- Indexes: `samples (user_id, type_id, start_at)`, `samples (user_id, natural_key)` unique, `samples (user_id, source_id, type_id)`, `workouts (user_id, start_at)`, `workouts (user_id, natural_key)` unique, `workouts (user_id, source_id)`, aggregates primary keys `(user_id, type_id, bucket_start)`.
- `metric_types` carries no `user_id`: it holds the catalog, which is instance-wide and holds no health data (FR-2).
- `sample_stats` (user, type, first start, last start, count), primary key `(user_id, type_id)`, holds the data availability of each user and type. It is maintained in the transaction of every write that adds, removes or moves samples, so that it never disagrees with `samples`: an insert updates the count and widens the first and last start, a removal lowers the count and, when it removed the first or last sample, reads the new bound from the index on `samples (user_id, type_id, start_at)`. An update that changes the start of a sample is a removal followed by an insert. A row whose count drops to zero is deleted. Account deletion is the only exception: the job removes the samples of a user being deleted in batches, then their rows of `sample_stats`, which nobody reads meanwhile. Counting the samples of ten years on each request would scan millions of index entries, while the first and last start alone are cheap through the index.
- `workouts` stores one row per workout, its values in canonical units: duration in seconds, pauses excluded, distance and elevation in meters, energy in kilocalories, heart rate in count/min. A total the workout lacks, and an unknown indoor flag, are `NULL`. The location type is `unknown`, `indoor`, `outdoor`, `pool` or `open_water`. The events are a JSON array of `{"type", "start", "end"}`, times in Unix milliseconds, of the types `pause`, `resume`, `lap`, `segment` and `marker`: an instant event ends when it starts. The import id references no table, as for samples. The pipeline computes the natural key and its rule ([Deduplication and source priority](data-deduplication-and-source-priority.md) FR-1): `update` replaces every field of the stored workout but its id, so that its route stays attached, and `ignore` keeps it. An upsert stores one workout and answers inserted, updated or duplicate with the id of the stored workout.
- Translations: a Go test checks that `web/src/locales/en/metrics.json` holds a name and a description for every curated key, and a translation for every value of every curated category type, and that `web/src/locales/en/activities.json` and `web/src/locales/fr/activities.json` hold a name for every activity key and no key that the activity type catalog lacks.
- Endpoints: `GET /api/v1/metrics` (the curated types, the uncurated types the user has samples of, and the data availability of the user from `sample_stats`: first and last sample, count), `GET /api/v1/metrics/{type}/series`, `GET /api/v1/metrics/{type}/samples`.

## Edge cases

- Daylight saving time: local days of 23 or 25 hours are handled because hourly buckets are stored as UTC instants.
- Timezones with 30 or 45 minute offsets: hourly buckets follow local hour boundaries, so daily totals are exact.
- Travel: samples keep their original offset for display, but "today" follows the user's timezone preference.
- No timezone yet (a user who has not logged in to the web app): aggregates use UTC until the user confirms one, then the change recomputes them.
- Samples far in the future or before 2000: accepted but flagged in the import report as suspicious.
- Category samples (for example symptoms) aggregate as counts and total duration per bucket.

## Acceptance criteria

- [ ] A one-year daily chart of steps loads in under 300 ms on a Raspberry Pi 4 with 10 years of data.
- [ ] Changing the timezone recomputes hourly and daily aggregates and Today values accordingly.
- [ ] An unknown type received in a payload appears in the metric explorer without code changes.
- [ ] Aggregates match a full recomputation after any sequence of imports and deletions (property-based test).

## Future ideas

- Optional 15-minute aggregates for detailed intraday charts.
- Postgres implementation of the storage interfaces.
