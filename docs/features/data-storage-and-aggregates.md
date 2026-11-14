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
  - A category sample has the value 1, and its category value is the key of a value listed by the type in the catalog (FR-1). A sample of an uncurated category type keeps the value as received. So does a sample of a curated type with a value that the catalog does not list, such as one added by a new iOS release: it is stored as the value of an uncurated type would be, and shown as received.
  - The import id references no table: it is kept for diagnosis, and purging import reports ([Ingestion pipeline](ingestion-pipeline.md) FR-11) never touches samples.
- **FR-4**: A workout stores: user, source, external id, activity type, start, end, duration, distance, active energy, total energy, elevation gain and loss, average and max heart rate, indoor flag, location type, events (pauses, laps, segments), metadata.
  - The activity type is a key from a catalog of activity types shipped with the binary (for example `running` for `HKWorkoutActivityTypeRunning`). Each activity type has a family from a closed list, which [Deduplication and source priority](data-deduplication-and-source-priority.md) FR-11 compares: `running`, `walking`, `hiking`, `cycling`, `swimming`, `rowing`, `strength`, `cardio`, `dance`, `mind_and_body`, `team_sport`, `racket_sport`, `winter_sport`, `water_sport`, `combat`, `other`. The list of families changes only with the code.
  - The activity type catalog (`internal/catalog/activities.yaml`) lists every value of `HKWorkoutActivityType` with its key, its name in the XML export, its HealthKit raw value and its family, and finds an activity type by any of the first three. An activity type that the catalog does not list, such as one added by a new iOS release, is stored as the activity `other`, of the family `other`, and counted as a warning in the import report ([Ingestion pipeline](ingestion-pipeline.md) FR-3). Names are not stored in the catalog: the web app translates the key in the i18next namespace `activities` (`web/src/locales/<language>/activities.json`), keyed by the activity key.
- **FR-5**: A route is stored once per workout as a compressed, columnar encoded blob (time, latitude, longitude, altitude, speed, course, horizontal and vertical accuracies), with its bounding box, total distance and a simplified polyline for list thumbnails. The encoding is defined in [ADR 0014](../decisions/0014-route-encoding.md): a route round-trips through it exactly at the precisions of that record.
- **FR-6**: Hourly buckets are aligned to the hours of the user's timezone (identical to UTC hours for whole-hour offsets). Daily buckets are the user's local days. Weekly, monthly and yearly values are computed on read from daily aggregates, using the user's week start.
- **FR-7**: Each aggregate row stores, after source selection: sum, count, min, max, last value and its time, the selected source, and for daily rows the number of hours with data.
  - What a row holds depends on the kind of its type:
    - cumulative: `sum` is the prorated total of the bucket (FR-8), `count` the number of samples that overlap it, and min, max and last are empty,
    - discrete: `sum` and `count` are those of the values of the samples that start in the bucket, a pre-aggregated point counting as one sample of its value. `min` and `max` come from the min and max of a point when it has them, and from its value otherwise. `last` is the value of the sample with the latest start, then the latest end, then the highest id, with its start as its time,
    - category: every value of the type counts together. `count` is the number of samples that overlap the bucket and `sum` the seconds of their overlap with it, prorated as a cumulative sample, and min, max and last are empty.
  - The mean is not stored: it is `sum / count`, so that days and weeks combine their hours and days exactly, without a mean of means. A daily row sums the `sum` and `count` of its hours, takes the smallest of their min, the largest of their max, and the last of its last hour with data.
  - An hourly row holds its selected source, or `combined` in combine mode ([Deduplication and source priority](data-deduplication-and-source-priority.md) FR-7). A daily row holds the source when every hour with data selected the same one, `combined` in combine mode, and `mixed` otherwise.
- **FR-8**: Cumulative samples spanning several buckets are prorated by overlap duration. Discrete samples are assigned to the bucket containing their start. An instantaneous cumulative or category sample, whose end is its start, counts in the bucket that holds its instant, with its whole value and no seconds.
- **FR-9**: The pipeline records dirty ranges per user and type. A recompute job merges overlapping ranges and recomputes the affected hours, then the affected days.
  - The transaction of each batch that adds, changes or removes samples inserts their ranges into `aggregate_dirty_ranges`, from the first start to the last end, those of a type less than an hour apart merged into one, so that a batch of frequent samples records a few ranges, and queues an `aggregate.recompute` job for the user unless one is queued and not yet running.
  - The job has the serial key `aggregate:<user id>`, so that one recomputation runs at a time per user. It shares no key with imports, because it reads committed samples only.
  - The job reads the ranges of the user present when it starts, widens them to the local hours that hold them, the end of a range included since it can be the instant of an instantaneous sample, merges the overlapping ones of each type and recomputes those hours, then the local days that hold them. It commits one local month per transaction, so that a long recomputation never holds the writer for long, and deletes the ranges it read once they are recomputed, through the largest id it read: ids never come back, so that a range recorded meanwhile stays. Ranges recorded meanwhile queue the next job.
  - The samples of a range are read through the index on `samples (user_id, type_id, start_at)`, from the start of the range less the longest duration of `sample_stats`, so that a cumulative sample that starts long before the range still counts in its hours.
  - A job whose user changes their effective timezone meanwhile stops before its next month, and leaves its ranges: the change queued the job that recomputes everything in the new timezone. Confirming UTC without a timezone changes nothing, since a user without one follows UTC.
- **FR-10**: Full recomputation for a user and type is triggered by: a change of source priority for that type, a change of the kind of a source that has samples of it, creation or removal of a block rule affecting it, a change of the user's timezone (all types).
  - A full recomputation records, for each type concerned, a dirty range from the first start of `sample_stats` to its last start widened by its longest duration, so that the range holds the end of the last sample, which FR-9 recomputes.
  - A change of timezone deletes the aggregate rows of the user and records the ranges of all their types in the transaction that stores the timezone, so that no row of the old timezone is read as one of the new one.
  - A deletion needs no full recomputation: the samples it removes record their ranges (FR-9). The ranges of a block rule are defined with [Deletion and block rules](data-deletion-and-block-rules.md).
- **FR-11**: Series read API: given type, period, bucket (`hour`, `day`, `week`, `month`, `year`, or `auto`) and statistic (`sum`, `mean`, `min`, `max`, `last`), returns points from aggregates. `auto` picks a bucket that yields at most about 400 points.
  - The type is the metric key, and the period is given by `from` and `to`, local dates of the user (`YYYY-MM-DD`), both included.
  - The statistics a kind accepts:

    | Kind | Statistics |
    |---|---|
    | Cumulative | `sum`. For `week`, `month` and `year` buckets also `mean`, `min` and `max`, over the daily sums of the bucket |
    | Discrete | `mean`, `min`, `max`, `last` |
    | Category | `sum` (seconds), `count` |

    Any other statistic answers `422` with the stable message `statistic_unsupported`. The mean of a cumulative type is over the days with data of the bucket.
  - `auto` picks the smallest of `hour`, `day`, `week`, `month` and `year` that yields at most 400 points, and `year` when none does. An explicit bucket that yields more than 5,000 points answers `422` with the stable message `too_many_points`, and so does `auto` when even `year` does. A `to` before `from` answers `422` with the stable message `period_invalid`.
  - Weeks start on Monday until the week start preference of [User preferences and profile](user-preferences-and-profile.md) (M2).
  - The answer holds the bucket used, the canonical unit, the points (start and value, empty buckets omitted) and `updating`, true when a range of `aggregate_dirty_ranges` of that user and type overlaps the period.
  - The start of a point is a UTC instant: the local hour, or the local midnight that starts its day, week, month or year. A week, month or year that starts before `from` or ends after `to` holds the days of the period only.
  - A curated type without samples of the user answers no points. An uncurated type without samples of the user answers `404`, as it is not listed to them (FR-2).
- **FR-12**: Raw samples API for short ranges, paginated, with an optional `maxPoints` parameter that downsamples with a shape-preserving algorithm (largest triangle three buckets) for raw line charts.
  - The range is given by `from` and `to`, RFC 3339 instants, `to` excluded. It holds the samples that start in it. A `to` that is not after `from` answers `422` with the stable message `period_invalid`. Samples are ordered by start, then id, and paginated by an opaque cursor, with `limit` from 1 to 5,000, 500 by default.
  - `maxPoints`, from 2 to 5,000, returns the whole range downsampled to at most that many samples, the first and last kept, without pagination, for quantity types only. The x of a sample is its start, and its y its value. With a cursor it answers `422` with the stable message `max_points_with_cursor`, and for a category type `max_points_unsupported`.
  - Each sample has its start, end, original UTC offset, value, the min and max of a pre-aggregated point, the category value of a category sample, its source (id and name shown to the user) and `usedInAggregates` ([Deduplication and source priority](data-deduplication-and-source-priority.md) FR-9): true when its source is the one `agg_hourly` holds for one of its local hours, in the timezone of the user, or when its type is in combine mode and its source is not ignored. The local hours of a sample are those it counts in (FR-8): the hour of its start for a discrete type, and every hour it overlaps for a cumulative or category type. A sample of hours without rows, such as one waiting for its recomputation, is false. The hourly rows of a page are read in one query.
  - A curated type without samples of the user answers no samples. An uncurated type without samples of the user answers `404`, as for the series (FR-11).
- **FR-13**: Charts and widgets read aggregates by default. Raw reads are limited to a configurable maximum range (for example 7 days).

  | Variable | YAML key | Default | Purpose |
  |---|---|---|---|
  | `VERVE_RAW_MAX_DAYS` | `raw_max_days` | `7` | Longest range of a raw samples read, in days, a whole number from 1 to 366 |

  A range longer than that many local days of the timezone of the user answers `422` with the stable message `range_too_long`: `to` may be at most the same local time that many days after `from`, so that a day across a change of offset counts as one.

## UX notes

- While aggregates are being recomputed after an import, affected charts show a small "updating" indicator rather than stale numbers without warning.

## Data and API notes

- Indexes: `samples (user_id, type_id, start_at)`, `samples (user_id, natural_key)` unique, `samples (user_id, source_id, type_id)`, `workouts (user_id, start_at)`, `workouts (user_id, natural_key)` unique, `workouts (user_id, source_id)`, `workout_routes (user_id)`, aggregates primary keys `(user_id, type_id, bucket_start)`.
- `metric_types` carries no `user_id`: it holds the catalog, which is instance-wide and holds no health data (FR-2).
- `agg_hourly` and `agg_daily` store the rows of FR-7, keyed by user, type and the start of their bucket as a UTC instant ([ADR 0012](../decisions/0012-local-hour-aggregates.md)): a local hour, or the local midnight that starts a day. The selected source is a source id, empty for `combined` and `mixed`, with a column telling the three apart. `aggregate_dirty_ranges` (user, type, from, to) holds the ranges of FR-9 until their recomputation. Account deletion removes the rows of the user from the three tables.
- `sample_stats` (user, type, first start, last start, count, longest duration), primary key `(user_id, type_id)`, holds the data availability of each user and type. The longest duration, in milliseconds, is that of the longest sample stored: inserts widen it, and removals leave it as an upper bound, which is all that the reads of FR-9 need. It is maintained in the transaction of every write that adds, removes or moves samples, so that it never disagrees with `samples`: an insert updates the count and widens the first and last start, a removal lowers the count and, when it removed the first or last sample, reads the new bound from the index on `samples (user_id, type_id, start_at)`. An update that changes the start of a sample is a removal followed by an insert. A row whose count drops to zero is deleted. Account deletion is the only exception: the job removes the samples of a user being deleted in batches, then their rows of `sample_stats`, which nobody reads meanwhile. Counting the samples of ten years on each request would scan millions of index entries, while the first and last start alone are cheap through the index.
- `workouts` stores one row per workout, its values in canonical units: duration in seconds, pauses excluded, distance and elevation in meters, energy in kilocalories, heart rate in count/min. A total the workout lacks, and an unknown indoor flag, are `NULL`. The location type is `unknown`, `indoor`, `outdoor`, `pool` or `open_water`. The events are a JSON array of `{"type", "start", "end"}`, times in Unix milliseconds, of the types `pause`, `resume`, `lap`, `segment` and `marker`: an instant event ends when it starts. The import id references no table, as for samples. The pipeline computes the natural key and its rule ([Deduplication and source priority](data-deduplication-and-source-priority.md) FR-1): `update` replaces every field of the stored workout but its id when one of them differs, the import id aside, so that its route stays attached, and `ignore` keeps it. An upsert stores one workout and answers inserted, updated or duplicate with the id of the stored workout.
- Routes are encoded by `internal/workouts/route` ([ADR 0014](../decisions/0014-route-encoding.md)). A route holds at most 1,048,576 points (2^20, about 12 days at one point per second): encoding refuses a longer one, and decoding refuses data that claims more, which bounds the memory that corrupt data takes. Encoding also refuses a latitude out of [-90, 90], a longitude out of [-180, 180] and a value that is not finite. The total distance is the sum of the haversine distances between consecutive points, on a sphere of 6,371,008.8 m (the mean radius of the Earth). The simplification for thumbnails starts at a tolerance of 5 m and caps the polyline at 250 points.
- `workout_routes` stores at most one route per workout, keyed by the workout and removed with it: the encoded route, its bounding box, total distance in meters, point count and Google encoded polyline, computed by `internal/workouts/route`, which the store does not import. It also carries the `user_id` of the workout, which scopes reads without a join, with an index on it for account deletion. Storing a route replaces the one the workout had, and the store refuses an empty encoding, a point count below 1, a distance that is negative or not finite, and a bounding box out of [-90, 90] and [-180, 180] or whose minimum is above its maximum. The workouts list reads the thumbnails of a page of workouts, polyline and bounding box without the encoded route.
- Translations: a Go test checks that `web/src/locales/en/metrics.json` holds a name and a description for every curated key, and a translation for every value of every curated category type, and that `web/src/locales/en/activities.json` and `web/src/locales/fr/activities.json` hold a name for every activity key and no key that the activity type catalog lacks.
- Endpoints: `GET /api/v1/metrics` (the curated types, the uncurated types the user has samples of, and the data availability of the user from `sample_stats`: first and last sample, count), `GET /api/v1/metrics/{type}/series`, `GET /api/v1/metrics/{type}/samples`.

## Edge cases

- Daylight saving time: local days of 23 or 25 hours are handled because hourly buckets are stored as UTC instants.
- Timezones with 30 or 45 minute offsets: hourly buckets follow local hour boundaries, so daily totals are exact.
- Travel: samples keep their original offset for display, but "today" follows the user's timezone preference.
- No timezone yet (a user who has not logged in to the web app): aggregates use UTC until the user confirms one, then the change recomputes them.
- Samples and workouts that start before 2000 or more than one day after the import was created: stored, and counted as `suspicious` in the import report with a sample error message ([Ingestion pipeline](ingestion-pipeline.md) FR-3).
- Category samples (for example symptoms) aggregate as counts and total duration per bucket (FR-7).
## Acceptance criteria

- [ ] A one-year daily chart of steps loads in under 300 ms on a Raspberry Pi 4 with 10 years of data.
- [ ] Changing the timezone recomputes hourly and daily aggregates and Today values accordingly.
- [ ] An unknown type received in a payload appears in the metric explorer without code changes.
- [ ] Aggregates match a full recomputation after any sequence of imports and deletions (property-based test with `pgregory.net/rapid`, which shrinks a failing sequence to a minimal one).

## Future ideas

- Optional 15-minute aggregates for detailed intraday charts.
- Postgres implementation of the storage interfaces.
