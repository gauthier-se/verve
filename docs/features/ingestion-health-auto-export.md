# Health Auto Export endpoint

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [API tokens](api-tokens.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Notifications](notifications.md)

## Summary

Continuous sync through the [Health Auto Export](https://apps.apple.com/app/id1115567069) iOS app, whose REST API automations POST JSON to a URL on a schedule. Verve exposes a dedicated endpoint that accepts the app's JSON export format version 2 and converts it into normalized records for the shared pipeline.

## User stories

- As a user, I want my Verve dashboard to stay up to date without manual exports.
- As a user, I want clear instructions on how to configure the app.
- As a user, I want to know when sync stops working.

## Scope

### In scope

- JSON export format version 2: `metrics`, `workouts`, `symptoms`, `cycleTracking`, `heartRateNotifications`.
- Summarized (aggregated) and unsummarized metric data.
- Batch requests.
- Recommended automation setup documentation.
- Sync freshness tracking per user.

### Out of scope

- Export format version 1 (rejected with a clear error asking to switch to version 2).
- CSV export format.
- `ecg`, `medications`, `stateOfMind` sections (counted as unsupported).

## Functional requirements

- **FR-1**: Endpoint `POST /api/v1/ingest/health-auto-export`, authenticated with an `ingest` token in the `Authorization` header configured in the app.
- **FR-2**: The token is checked before the body is read. The body is streamed to the inbox (bounded by `VERVE_MAX_INGEST_BODY`) and the server responds `202 Accepted` quickly, because the app's background tasks only have about 30 seconds.
- **FR-3**: Headers sent by the app are recorded on the import: `automation-name`, `automation-id`, `automation-aggregation`, `automation-period`, `session-id`. Batched requests sharing a `session-id` are grouped in the import history.
- **FR-4**: Body structure: a top-level `data` object with arrays per data type. Dates use `yyyy-MM-dd HH:mm:ss Z`.
- **FR-5**: Metrics: each metric has `name` (snake_case, for example `step_count`, `active_energy`, `heart_rate`, `weight_body_mass`), `units` and `data` points. The adapter maps names to catalog types through a mapping table maintained in the catalog.
- **FR-6**: Data point shapes handled:

  | Shape | Handling |
  |---|---|
  | `qty`, `date`, optional `source` | Quantity sample |
  | `heart_rate`: `Min`, `Avg`, `Max` | Pre-aggregated sample with value `Avg`, plus min and max |
  | `blood_pressure`: `systolic`, `diastolic` | Two samples (systolic, diastolic) |
  | `sleep_analysis` unsummarized: `startDate`, `endDate`, `value` | Category samples with stage (Awake, Core, Deep, REM, Asleep unspecified, In Bed) |
  | `sleep_analysis` summarized: per-day totals | Stored as daily sleep totals only, with a warning in the report that stages need the unsummarized setting |
  | `blood_glucose` with `mealTime`, `insulin_delivery` with `reason`, `handwashing` and `toothbrushing` with `value` | Quantity samples with the extra field in metadata |

- **FR-7**: Summarized data: when the app summarizes metrics, each point represents a time bucket (minute, hour, day...) taken from `automation-aggregation`. The adapter sets the end of the interval from that grouping. Points are attached to a per-user synthetic source "Health Auto Export (summarized)" of kind `aggregated`, because HealthKit already deduplicated them across devices. The original `source` string is kept in metadata.
- **FR-8**: Summarized points are upserted on `(user, type, synthetic source, start, end)`: a bucket sent again with a new value (for example the current hour, resent at the next sync) replaces the previous value.
- **FR-9**: Unsummarized points use the regular natural key and the source given in `source`. When `source` is missing (possible on iOS 27 and later for cumulative and heart rate data), they are attached to a per-user source "Unknown (Health Auto Export)".
- **FR-10**: Workouts (version 2): `id`, `name`, `start`, `end`, `duration`, `location`, `isIndoor`, `distance`, `activeEnergyBurned`, `totalEnergy`, `intensity`, `elevationUp`, `elevationDown`, `temperature`, `humidity`, speed and heart rate summaries, `metadata`. Workouts are idempotent on `(user, id)`.
- **FR-11**: Workout time series: `heartRateData` and `heartRateRecovery` items are stored as heart rate samples (pre-aggregated per grouping) on the workout's source. `stepCount` and other arrays are stored as samples of their types. `route` points (`latitude`, `longitude`, `altitude`, `speed`, `course`, accuracies, `timestamp`) become the workout route.
- **FR-12**: Symptoms, cycle tracking and heart rate notifications are stored as category samples of their catalog types.
- **FR-13**: Each user has a "last successful sync" timestamp per automation (by `automation-id`). The [Notifications](notifications.md) feature uses it for the "sync stale" event.
- **FR-14**: The setup guide in the app recommends these automations, all with format JSON, export version 2, date range "Since Last Sync", batch requests on, and the `Authorization: Bearer <token>` header:

  | Automation | Data | Settings | Cadence |
  |---|---|---|---|
  | Verve metrics | Health Metrics, all except sleep analysis | Summarize data on, grouped by hour | Every hour |
  | Verve sleep | Health Metrics, sleep analysis only | Summarize data off | Daily, mid-morning |
  | Verve workouts | Workouts, with route and heart rate data | Heart rate grouped by minute | Every few hours |
  | Verve events | Symptoms, cycle tracking, heart rate notifications | Default | Daily |

- **FR-15**: The guide explains trade-offs: hourly grouping keeps payloads small but limits intraday resolution; minute grouping for heart rate gives finer charts with larger payloads.

## UX notes

- "Connect Health Auto Export" wizard: create token, show URL and header, show the recommended automations, then wait for the first request and confirm "First data received".
- Sync status widget in settings and on the Today page (see [Today page](page-today.md)): last sync per automation, with warning colors.

## Data and API notes

- Catalog mapping table between Health Auto Export metric names and HealthKit identifiers, covered by tests with fixture payloads.
- Units are taken from the payload (`units`) because the app lets users choose unit preferences.

## Edge cases

- iPhone locked when the automation runs: HealthKit is unavailable and the app sends nothing or an empty payload. "Since Last Sync" fills the gap on the next run.
- Payloads of several hundred megabytes after a long gap: streamed to disk, then processed.
- The same data received summarized (Health Auto Export) and raw (XML import) for the same hours: handled by per-bucket source selection, no double counting.
- Duplicate deliveries by the app's retries: absorbed by idempotency.
- Version 1 payload (detected by `lat`/`lon` route fields or `activeEnergy` in workouts): rejected with `422` and a message explaining how to switch to version 2.

## Acceptance criteria

- [ ] Fixture payloads (summarized, unsummarized, workouts with routes, batched) import correctly.
- [ ] Resending a summarized hour with a larger value updates it instead of duplicating it.
- [ ] The endpoint responds in under 2 seconds for a 100 MB payload on a Raspberry Pi 4 (processing continues asynchronously).
- [ ] Combining an XML import and Health Auto Export data for the same period does not double count steps.

## Future ideas

- Support for `stateOfMind` and `medications` sections.
- Automatic detection of a misconfigured automation (for example summarized sleep) with a suggestion in the UI.
