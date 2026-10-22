# Storage and aggregates

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Metric explorer](page-metric-explorer.md), [ADR 0001](../decisions/0001-sqlite-behind-storage-interface.md), [ADR 0008](../decisions/0008-generic-sample-storage.md)

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

- **FR-1**: The catalog ships with the binary and lists known HealthKit quantity and category types with: key (for example `heart_rate`), HealthKit identifier, kind (`quantity` or `category`), canonical unit, aggregation style (`cumulative` such as steps and energy, or `discrete` such as heart rate and weight), display group (Activity, Heart, Body, Sleep, Nutrition, Respiratory, Vitals, Mobility, Hearing, Cycle, Symptoms, Other), translatable name and description, and for categories the list of values.
- **FR-2**: Types not in the catalog are created on first sight as "uncurated" with the received unit, kind inferred from the payload and a default aggregation style of `discrete`. They appear in the metric explorer under "Other".
- **FR-3**: A sample stores: user, type, source, start (UTC), end (UTC), original UTC offset, numeric value in canonical unit, optional min and max (pre-aggregated points), optional category value, origin, import id, optional external uuid, natural key hash and optional metadata (JSON).
- **FR-4**: A workout stores: user, source, external id, activity type, start, end, duration, distance, active energy, total energy, elevation gain and loss, average and max heart rate, indoor flag, location type, events (pauses, laps, segments), metadata.
- **FR-5**: A route is stored once per workout as a compressed, columnar encoded blob (time, latitude, longitude, altitude, speed, accuracies), with its bounding box, total distance and a simplified polyline for list thumbnails.
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

- Indexes: `samples (user_id, type_id, start_at)`, `samples (user_id, natural_key)` unique, `samples (user_id, source_id, type_id)`, aggregates primary keys `(user_id, type_id, bucket_start)`.
- Endpoints: `GET /api/v1/metrics` (catalog with per-user data availability: first and last sample, count), `GET /api/v1/metrics/{type}/series`, `GET /api/v1/metrics/{type}/samples`.

## Edge cases

- Daylight saving time: local days of 23 or 25 hours are handled because hourly buckets are stored as UTC instants.
- Timezones with 30 or 45 minute offsets: hourly buckets follow local hour boundaries, so daily totals are exact.
- Travel: samples keep their original offset for display, but "today" follows the user's timezone preference.
- Samples far in the future or before 2000: accepted but flagged in the import report as suspicious.
- Category samples (for example symptoms) aggregate as counts and total duration per bucket.

## Acceptance criteria

- [ ] A one-year daily chart of steps loads in under 300 ms on a Raspberry Pi 4 with 10 years of data.
- [ ] Changing the timezone recomputes daily aggregates and Today values accordingly.
- [ ] An unknown type received in a payload appears in the metric explorer without code changes.
- [ ] Aggregates match a full recomputation after any sequence of imports and deletions (property-based test).

## Future ideas

- Optional 15-minute aggregates for detailed intraday charts.
- Postgres implementation of the storage interfaces.
