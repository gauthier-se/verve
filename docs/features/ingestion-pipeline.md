# Ingestion pipeline and native API

- **Status:** Planned
- **Milestone:** M1 (FR-12 in M3)
- **Related:** [XML import](ingestion-xml-import.md), [Health Auto Export endpoint](ingestion-health-auto-export.md), [Storage and aggregates](data-storage-and-aggregates.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Deletion and block rules](data-deletion-and-block-rules.md), [ADR 0007](../decisions/0007-ingestion-strategy.md)

## Summary

Every way of getting data into Verve goes through one pipeline that works on normalized records. Source-specific adapters (Apple Health XML export, Health Auto Export, Verve native format) only parse and convert. The pipeline handles type mapping, units, sources, block rules, deduplication, batched writes, aggregate invalidation and import reports. Verve also exposes its own versioned native ingest format, which the future iOS companion app will use.

## User stories

- As a user, I want every import to behave the same way regardless of where the data comes from.
- As a user, I want a report for each import telling me what was added, ignored or rejected.
- As the future iOS companion app, I want a stable format that supports HealthKit UUIDs and deletions.

## Scope

### In scope

- Normalized record model (quantity sample, category sample, workout with route).
- Shared pipeline stages.
- Native ingest endpoint and format version 1.
- Import history and reports.
- Per-user import concurrency control.

### Out of scope

- Source-specific parsing (in the adapter feature files).
- Writing data back to Apple Health (Verve never writes to HealthKit).

## Functional requirements

- **FR-1**: Adapters produce a stream of normalized records. They never write to storage directly.
- **FR-2**: Pipeline stages, in order:
  1. **Type mapping**: map the incoming identifier (HealthKit identifier, Health Auto Export name, native type) to a catalog metric type. Unknown types are added to the catalog with their kind and unit, flagged as "uncurated".
  2. **Unit conversion**: convert values to the canonical unit of the metric type. A value with an unknown or incompatible unit is rejected as invalid and counted.
  3. **Time parsing**: parse start and end with their UTC offset, store UTC plus the original offset. Records where end is before start are invalid.
  4. **Source resolution**: find or create the source for the user (see [Deduplication and source priority](data-deduplication-and-source-priority.md)).
  5. **Block rules**: drop records matching an active rule and count them as blocked.
  6. **Deduplication**: compute the natural key and upsert according to the record origin.
  7. **Batched writes**: write in transactions of bounded size (for example 5,000 records) to keep the single SQLite writer available for API requests.
  8. **Dirty ranges**: record affected `(type, time range)` per user and enqueue aggregate recomputation once the batch is committed.
  9. **Report**: update counters on the import record.
- **FR-3**: Import report counters: received, inserted, updated, duplicates, blocked, invalid, unsupported (known but excluded types such as ECG), with up to 100 sample error messages kept for diagnosis.
- **FR-4**: Only one import runs at a time per user. Concurrent pushes are queued as jobs and processed in arrival order, with a per-user serial key (see [Job worker](platform-jobs.md) FR-6). An import waiting for a retry does not hold back the next ones.
- **FR-5**: Imports are resumable at batch granularity: if the process stops, the job restarts and duplicates are absorbed by idempotency.
- **FR-6**: Native endpoint `POST /api/v1/ingest` (scope `ingest`):
  - the body is persisted durably to the inbox, then the server responds `202 Accepted` with an import id,
  - `GET /api/v1/imports/{id}` returns status and report,
  - a `202` means the payload is safely stored and will be processed.
- **FR-7**: Native format version 1 (`format: "verve.ingest.v1"`) contains:

  | Field | Content |
  |---|---|
  | `format` | Format identifier and version |
  | `client` | Client name, version, device model, OS version |
  | `samples` | Quantity and category samples: `uuid` (optional but recommended), HealthKit type identifier, start and end with offset, value and unit, or category value, source (name, bundle id, device name, model, hardware and software version), metadata |
  | `workouts` | `uuid`, activity type, start, end, duration, totals (distance, active energy, total energy, elevation), indoor flag, source, metadata, events (pauses, laps, segments), route points (time, latitude, longitude, altitude, speed, course, accuracies) |
  | `deletions` | List of `uuid` deleted on the device since the last sync |

- **FR-8**: Samples with a `uuid` are idempotent on `(user, uuid)`, workouts on `(user, origin, uuid)`, as in [Deduplication and source priority](data-deduplication-and-source-priority.md) FR-1. Deletions remove the matching samples or workouts and do not create block rules, since the deletion happened at the source.
- **FR-9**: Each import records its origin (`xml`, `hae`, `native`, `cli`), the token used if any, client metadata, timestamps and status (`queued`, `running`, `completed`, `failed`, `cancelled`).
- **FR-10**: Users can cancel a queued or running import. Data already committed stays.
- **FR-11**: Import history keeps the last 200 imports per user, older reports are purged.
- **FR-12** (M3): When an import completes or fails, a notification event is emitted (see [Notifications](notifications.md)).

## UX notes

- "Imports" page: TanStack Table of imports with origin, automation name, date, duration, status and counters. A detail drawer shows per-type counts and sample errors.
- A banner on every page while an import of the current user is running, with progress when known.

## Data and API notes

- Tables: `imports`, `jobs`, inbox files in `/data/inbox`.
- Inbox files are deleted after successful processing, and kept 7 days after a failure for retry or diagnosis.

## Edge cases

- Payload with mixed valid and invalid records: valid ones are stored, invalid ones counted.
- Unit mismatch for the same type across sources (kcal and kJ): both converted to canonical.
- Server restart during processing: the job resumes from the inbox file.
- Disk full while writing to the inbox: respond `507 Insufficient Storage` so the client retries later.

## Acceptance criteria

- [ ] Ingesting the same payload twice produces zero inserts the second time.
- [ ] A crash in the middle of an import leaves a consistent database, and the job completes after restart.
- [ ] Block rules apply identically to XML, Health Auto Export and native imports.
- [ ] Native deletions remove the referenced records.

## Future ideas

- Pairing flow for the companion app.
- Server-sent events for live import progress instead of polling.
