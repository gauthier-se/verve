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
  1. **Type mapping**: map the incoming identifier (HealthKit identifier, Health Auto Export name, native type) to a catalog metric type. Identifiers that the catalog lists as known but unsupported are counted as unsupported and dropped. Other unknown types are added to the catalog with their kind and unit, flagged as "uncurated" (see [Storage and aggregates](data-storage-and-aggregates.md) FR-1 and FR-2).
  2. **Unit conversion**: convert values to the canonical unit of the metric type, with the unit strings of the [Units table](../architecture.md#units), matched exactly as HealthKit writes them. A value of a curated type with a unit that the table does not list, or lists for another dimension, is rejected as invalid and counted. An uncurated type stores the unit of its first record, and converts the later ones to it as the Units section describes. A workout converts its distance and elevations to m, its energies to kcal, its heart rates to count/min and its duration to s. A workout whose adapter gives no duration gets its time range without its pauses, each from a pause or motion pause event to the next resume or motion resume, or to the end of the workout.
  3. **Time parsing**: parse start and end with their UTC offset, store UTC plus the original offset. Records where end is before start are invalid. A sample or workout that starts before 2000-01-01T00:00:00Z, or more than one day after the import was created, is suspicious: it is stored and counted as `suspicious` (FR-3), with a sample error message.
  4. **Source resolution**: find or create the source for the user (see [Deduplication and source priority](data-deduplication-and-source-priority.md)).
  5. **Block rules**: drop records matching an active rule and count them as blocked. Until [Deletion and block rules](data-deletion-and-block-rules.md) comes with M1-E11, the stage hands each record to a rule set that blocks nothing.
  6. **Deduplication**: compute the natural key and upsert according to the record origin.
  7. **Batched writes**: write in transactions of bounded size (for example 5,000 records) to keep the single SQLite writer available for API requests.
  8. **Dirty ranges**: record affected `(type, time range)` per user and enqueue aggregate recomputation once the batch is committed. Until the tables of dirty ranges come with the aggregates of M1-E8, the stage collects the ranges and hands them to an interface that does nothing, as block rules do until M1-E11.
  9. **Report**: update counters on the import record.
- **FR-3**: Import report counters, each kept per type key as well as in total, with up to 100 sample error messages kept for diagnosis. A sample error message names the position and the type of its record and the reason, never a value.

  | Counter | Counts |
  |---|---|
  | `received` | Samples and workouts read from the payload |
  | `inserted` | Records stored for the first time |
  | `updated` | Records that replaced a stored one ([Deduplication and source priority](data-deduplication-and-source-priority.md) FR-1) |
  | `duplicates` | Records identical to a stored one |
  | `blocked` | Records dropped by a block rule |
  | `invalid` | Records rejected by a stage, such as a missing field or a unit that the Units table does not list |
  | `unsupported` | Records of known but excluded types such as ECG, listed by the catalog in [Storage and aggregates](data-storage-and-aggregates.md) FR-1 |
  | `suspicious` | Stored records that start before 2000 or more than one day after the import was created (FR-2 stage 3), also counted as inserted, updated or duplicate |
  | `warnings` | Stored records that lost a detail, such as a workout whose activity type the activity type catalog does not list, stored as `other` ([Storage and aggregates](data-storage-and-aggregates.md) FR-4) |
  | `deleted` | Records removed by a deletion (FR-8) |
  | `notFound` | Deleted uuids that matched no record (FR-8) |

  - The type key of a sample is the key of its metric type, and of a record of an unsupported type the identifier as received. Workouts are counted apart from the types, under `workouts`. A record without a usable type, such as a sample whose `type` is missing, and a `notFound` deletion count in the total only. A sample that its adapter cannot read, such as one whose `value` is a string, counts under the type key of its type when that type is already stored, and in the total only otherwise: an invalid record never creates a type, and one of an unsupported type counts as `unsupported`, as type mapping comes first. A deletion that is not a uuid counts as `invalid` in the total only, and never as `received`.
  - A record counted as `suspicious` or under `warnings` also gets a sample error message, while fewer than 100 are kept.
- **FR-4**: Only one import runs at a time per user. Concurrent pushes are queued as jobs and processed in arrival order, with a per-user serial key, `import:<user id>`, shared by every import kind (see [Job worker](platform-jobs.md) FR-6). An import waiting for a retry does not hold back the next ones.
- **FR-5**: Imports resume where they stopped. The transaction of each batch also stores on the import the position of the next record, the counters and the sample error messages. A job that resumes an import skips the records before that position, so that its report equals the one of an uninterrupted run. The position is defined by the adapter, such as the index of a record for the native format (FR-7) or a byte offset for the XML export, and is the same on every run of a payload. Idempotency still absorbs a payload that a client sends twice.
- **FR-6**: Native endpoint `POST /api/v1/ingest` (scope `ingest`):
  - before answering, the server checks only the token and the size of the body, bounded by `VERVE_MAX_INGEST_BODY`: a larger body answers 413 and leaves no file. It never parses the body, since a body of 1 GB cannot be parsed within the time of a request,
  - the body is persisted durably to the inbox (see Data and API notes), then the server responds `202 Accepted` with the import, as `GET /api/v1/imports/{id}` returns it, its status `queued`,
  - a `202` means the payload is safely stored and will be processed,
  - the job `import.native` validates the body. A body that is not valid JSON, or not a payload of `verve.ingest.v1`: not an object, without its `client` or `format`, with a member of the payload twice, with a `format` other than `verve.ingest.v1`, or whose `samples`, `workouts` or `deletions` is not an array, fails the import at once, without retry, with the reason as its sample error message. The job checks the whole file in a first streaming pass, before it reads any record, so that such a body stores nothing,
  - the client reads the outcome from `GET /api/v1/imports/{id}`.
- **FR-7**: Native format version 1. Field names are in camelCase. Types, category values and workout activity types are HealthKit identifiers (`HKQuantityTypeIdentifierStepCount`, `HKCategoryValueSleepAnalysisAsleepCore`, `HKWorkoutActivityTypeRunning`), and units are HealthKit unit strings, listed in the [Units table](../architecture.md#units), so that a client forwards HealthKit as it reads it, without a mapping of its own.
  - A time is an RFC 3339 string with a mandatory offset, `Z` or `+01:00`, and optional fractional seconds. A time without offset is invalid. The offset is the original UTC offset of the record (FR-2 stage 3).
  - A uuid is the 36-character text form of a UUID, as HealthKit writes it, compared whatever its case. A uuid of another form makes its record invalid.
  - A quantity is an object `{"value": 5.2, "unit": "km"}`: `value` is a finite number, `unit` a unit string.
  - Metadata is an object whose values are strings, numbers or booleans, stored as received. A nested object, an array or `null` makes its record invalid.
  - Members that the format does not define are ignored, so that a newer client can add some. A missing required field, or a field of the wrong type, makes its record invalid, and the other records are read. A member that the format defines, outside metadata, counts as left out when its value is `null`.
  - Members come in any order: the first pass of the job (FR-6) also finds where each member starts in the inbox file. Records are read from `samples`, then `workouts`, then `deletions`, so that the deletions apply after the samples and workouts (FR-8), and the position of a record (FR-5) is its index in that order.

  Payload:

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `format` | string | yes | `verve.ingest.v1` |
  | `client` | client | yes | The client that sent the payload |
  | `samples` | array of sample | no | Quantity and category samples |
  | `workouts` | array of workout | no | Workouts with their events and route |
  | `deletions` | array of string | no | The uuids deleted on the device since the last sync |

  Client:

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `name` | string | yes | Client name, for example `Verve for iOS` |
  | `version` | string | yes | Client version |
  | `deviceModel` | string | no | Model of the device that sent the payload |
  | `osVersion` | string | no | OS version of that device |

  Sample. A sample with `value` and `unit` is a quantity sample, and one with `categoryValue` a category sample. A sample with both, or neither, is invalid, as is a quantity sample of a curated category type or a category sample of a curated quantity type.

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `uuid` | string | no, recommended | HealthKit uuid of the sample, which makes it idempotent and deletable (FR-8) |
  | `type` | string | yes | HealthKit type identifier |
  | `start` | time | yes | Start of the sample |
  | `end` | time | yes | End of the sample, equal to the start for an instant |
  | `value` | number | quantity sample | Value in `unit` |
  | `unit` | string | quantity sample | HealthKit unit string |
  | `categoryValue` | string | category sample | HealthKit category value identifier |
  | `source` | source | yes | Where the sample comes from |
  | `metadata` | metadata | no | HealthKit metadata of the sample |

  Source, as HealthKit gives it by the source revision and the device of a sample or workout. Its identity is the name and the device model ([Deduplication and source priority](data-deduplication-and-source-priority.md) FR-2).

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `name` | string | yes | Name of the source, for example `Apple Watch` |
  | `bundleId` | string | no | Bundle id of the app that wrote the record |
  | `device` | device | no | The device that recorded it |

  Device:

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `name` | string | no | Device name |
  | `manufacturer` | string | no | Manufacturer |
  | `model` | string | no | HealthKit device model, for example `Watch` or `iPhone` |
  | `hardwareVersion` | string | no | Hardware version, for example `Watch7,1` |
  | `softwareVersion` | string | no | Software version |

  Workout:

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `uuid` | string | yes | HealthKit uuid of the workout |
  | `activityType` | string | yes | HealthKit activity type identifier |
  | `start` | time | yes | Start of the workout |
  | `end` | time | yes | End of the workout |
  | `duration` | number | yes | Duration in seconds, pauses excluded |
  | `totals` | totals | no | Totals of the workout |
  | `indoor` | boolean | no | Indoor flag, unknown when left out |
  | `source` | source | yes | Where the workout comes from |
  | `metadata` | metadata | no | HealthKit metadata of the workout |
  | `events` | array of event | no | Pauses, laps, segments and markers |
  | `route` | array of route point | no | Route of the workout, in time order |

  Totals, each a quantity whose unit has the dimension of its total (energy, or length for the distance and the elevation), and each left out when the workout lacks it:

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `distance` | quantity | no | Total distance |
  | `activeEnergy` | quantity | no | Active energy burned |
  | `totalEnergy` | quantity | no | Active and basal energy burned |
  | `elevationAscended` | quantity | no | Elevation gain |
  | `elevationDescended` | quantity | no | Elevation loss |

  Event. The types `HKWorkoutEventTypePause`, `HKWorkoutEventTypeResume`, `HKWorkoutEventTypeLap`, `HKWorkoutEventTypeSegment` and `HKWorkoutEventTypeMarker` are stored as `pause`, `resume`, `lap`, `segment` and `marker`, and `HKWorkoutEventTypeMotionPaused` and `HKWorkoutEventTypeMotionResumed` as `pause` and `resume`. `HKWorkoutEventTypePauseOrResumeRequest` is ignored. Another type is ignored too, and its workout counted under `warnings` (FR-3).

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `type` | string | yes | HealthKit workout event type identifier |
  | `start` | time | yes | Start of the event |
  | `end` | time | yes | End of the event, equal to the start for an instant event |

  Route point, as CoreLocation gives it. A negative speed, course or accuracy, which CoreLocation writes when the value is unknown, counts as left out. A latitude or longitude out of its range, or a time outside the time range of the workout, makes the workout invalid.

  | Field | Type | Required | Content |
  |---|---|---|---|
  | `time` | time | yes | Time of the point |
  | `latitude` | number | yes | Latitude in degrees, from -90 to 90 |
  | `longitude` | number | yes | Longitude in degrees, from -180 to 180 |
  | `altitude` | number | no | Altitude in meters |
  | `speed` | number | no | Speed in meters per second |
  | `course` | number | no | Course in degrees from true north |
  | `horizontalAccuracy` | number | no | Horizontal accuracy in meters |
  | `verticalAccuracy` | number | no | Vertical accuracy in meters |

  Example payload:

  ```json
  {
    "format": "verve.ingest.v1",
    "client": {"name": "Verve for iOS", "version": "1.0.0", "deviceModel": "iPhone17,1", "osVersion": "27.1"},
    "samples": [
      {
        "uuid": "6F1C2B9E-3D4A-4E0B-9C59-1A2B3C4D5E6F",
        "type": "HKQuantityTypeIdentifierStepCount",
        "start": "2026-11-10T08:00:00+01:00",
        "end": "2026-11-10T08:10:00+01:00",
        "value": 1240,
        "unit": "count",
        "source": {
          "name": "Apple Watch",
          "bundleId": "com.apple.health.81C9A0F2-6E43-4A6B-9D8E-2B7C5F1A0E93",
          "device": {"name": "Apple Watch", "manufacturer": "Apple Inc.", "model": "Watch", "hardwareVersion": "Watch7,1", "softwareVersion": "12.1"}
        }
      },
      {
        "uuid": "0B8D4E21-77C3-4F5A-8E16-9A3D2C1B0F4E",
        "type": "HKQuantityTypeIdentifierBloodGlucose",
        "start": "2026-11-10T07:30:00+01:00",
        "end": "2026-11-10T07:30:00+01:00",
        "value": 5.4,
        "unit": "mmol<180.15588000005408>/L",
        "source": {"name": "Glucose Diary", "bundleId": "com.example.glucosediary"},
        "metadata": {"HKBloodGlucoseMealTime": 1, "HKWasUserEntered": true}
      },
      {
        "uuid": "C3A9F5D7-1E2B-4C8A-B6D4-5F7E9A1C3B2D",
        "type": "HKCategoryTypeIdentifierSleepAnalysis",
        "start": "2026-11-10T01:12:00+01:00",
        "end": "2026-11-10T02:03:30.250+01:00",
        "categoryValue": "HKCategoryValueSleepAnalysisAsleepCore",
        "source": {
          "name": "Apple Watch",
          "bundleId": "com.apple.health.81C9A0F2-6E43-4A6B-9D8E-2B7C5F1A0E93",
          "device": {"model": "Watch"}
        }
      }
    ],
    "workouts": [
      {
        "uuid": "9E7D5C3B-1A2F-4E6D-8C0B-2F4A6E8C0D1B",
        "activityType": "HKWorkoutActivityTypeRunning",
        "start": "2026-11-10T18:00:00+01:00",
        "end": "2026-11-10T18:31:00+01:00",
        "duration": 1800,
        "totals": {
          "distance": {"value": 5.21, "unit": "km"},
          "activeEnergy": {"value": 362, "unit": "kcal"},
          "totalEnergy": {"value": 401, "unit": "kcal"},
          "elevationAscended": {"value": 42, "unit": "m"}
        },
        "indoor": false,
        "source": {
          "name": "Apple Watch",
          "bundleId": "com.apple.health.81C9A0F2-6E43-4A6B-9D8E-2B7C5F1A0E93",
          "device": {"model": "Watch"}
        },
        "metadata": {"HKAverageMETs": "9.8 kcal/hr·kg"},
        "events": [
          {"type": "HKWorkoutEventTypePause", "start": "2026-11-10T18:12:00+01:00", "end": "2026-11-10T18:12:00+01:00"},
          {"type": "HKWorkoutEventTypeResume", "start": "2026-11-10T18:13:00+01:00", "end": "2026-11-10T18:13:00+01:00"}
        ],
        "route": [
          {"time": "2026-11-10T18:00:01+01:00", "latitude": 48.8566, "longitude": 2.3522, "altitude": 35.2, "speed": 2.9, "course": 87.5, "horizontalAccuracy": 4.1, "verticalAccuracy": 3.2},
          {"time": "2026-11-10T18:00:02+01:00", "latitude": 48.85662, "longitude": 2.35224, "altitude": 35.3, "speed": -1, "course": -1, "horizontalAccuracy": 4.0, "verticalAccuracy": 3.1}
        ]
      }
    ],
    "deletions": ["5A4B3C2D-1E0F-4A9B-8C7D-6E5F4A3B2C1D"]
  }
  ```

- **FR-8**: Samples with a `uuid` are idempotent on `(user, uuid)`, workouts on `(user, origin, uuid)`, as in [Deduplication and source priority](data-deduplication-and-source-priority.md) FR-1. Deletions:
  - a deleted uuid removes the samples of the user with that uuid, whatever their origin, and the workouts of the user with that uuid and the origin `native`, with their routes,
  - deletions apply after the samples and workouts of the same payload, so that a record sent and deleted in one payload ends up deleted,
  - each record removed counts as `deleted` (FR-3), under its type key or under `workouts`, and a uuid that removes nothing counts once as `notFound`. A `notFound` deletion is expected, since a client may delete a record that it never sent,
  - a removal updates the data availability (`sample_stats`) of the samples it removes and records dirty ranges, as any removal does,
  - deletions never create block rules, since the deletion happened at the source.
- **FR-9**: Each import records its origin (`xml`, `hae`, `native`, `cli`), the token used if any, client metadata, timestamps and status (`queued`, `running`, `completed`, `failed`, `cancelled`). A native push records the `client` of its payload once its job reads it, since the request never parses the body.
- **FR-10**: Users can cancel a queued or running import. Data already committed stays.
- **FR-11**: Import history keeps the last 200 imports per user. The transaction that creates an import purges the finished imports (`completed`, `failed` and `cancelled`) of the user that are not among their 200 newest imports. Queued and running imports are never purged. Samples keep their import id, which references no table, so purging a report never touches them.
- **FR-12** (M3): When an import completes or fails, a notification event is emitted (see [Notifications](notifications.md)).

## UX notes

- "Imports" page (`/imports`, in the main navigation): TanStack Table of the imports of the user, newest first, with origin, client (the name and version of its `client`, and the name of its token), creation date, duration (from its first run to its end), status and the counters `received`, `inserted`, `updated`, `duplicates` and `invalid` of `total`. The next pages load on demand, with a "Show more" button. Without any import, the page points to the API tokens page.
- A detail drawer per import reads `GET /api/v1/imports/{id}` and shows every counter of `total`, the counters of each type key of the report (FR-3), the types with the most records first and the workouts last, and the sample errors. A type key is shown by the translated name of its metric type, by the identifier of an uncurated type, as `GET /api/v1/metrics` gives it, and as it is for an unsupported type, whose key is its identifier. A queued or running import offers to cancel it (FR-10), behind a confirmation.
- A banner on every page of a session while an import of the current user is running, with the number of records received so far once there is one, and a link to the imports page. It polls `GET /api/v1/imports?status=running` every 5 seconds while an import runs, and every minute otherwise.

## Data and API notes

- Tables: `imports`, `jobs`, inbox files in `/data/inbox`.
- `imports` has an index on `(user_id, created_at, id)`, which serves the list, the purge of FR-11 and account deletion. Its `user_id` references the user, and its `token_id` references no table, so that revoking a token keeps the history. Its id is never reused, so that the import id that samples and workouts keep never names a later import. The report is stored as JSON: the total counters, the counters of each type key and of the workouts, a counter at zero left out, and the sample error messages, of which the store keeps the first 100. The status moves from `queued` to `running`, which stores the time the import first ran and stays set when a resumed job runs it again, then to `completed`, `failed` or `cancelled`, which stores the time it ended. A queued import can end without running, and a finished import cannot change status again. The position and the report are saved only on a running import.
- Inbox: a push endpoint writes the body to a temporary file in `/data/inbox`, syncs it, renames it to its final name, a random name that the import stores, and syncs the directory. Only then does it create the import and its job in one transaction ([Job worker](platform-jobs.md) FR-2), and answer `202`. A crash before that commit leaves at most a file without import, which `cleanup.inbox` removes. A disk full at any step, the transaction included, answers `507 Insufficient Storage` and removes the file, so that the client retries later.
- Inbox files are deleted once their import completes or is cancelled, and kept 7 days after a failure for retry or diagnosis. The recurring job `cleanup.inbox` runs daily and removes, once they are older than one hour, the files of imports that failed more than 7 days ago, the files of completed and cancelled imports that their removal missed, such as a removal that failed after a cancellation committed, and the files without import, temporary ones included. A failed import that the history purges before its 7 days (FR-11) thus loses its file at the next cleanup.
- An import job ends its import: `completed` once every record is processed, `failed` without retry for a payload that fails as a whole (FR-6) or an inbox file that is gone, with the reason as its sample error message, `failed` once the last attempt of its job fails ([Job worker](platform-jobs.md) FR-5), with a sample error message that says so, and `cancelled` at the next batch boundary once its job is cancelled, also when the run fails or stops, or has not started the import yet, before the cancellation reaches it: its job, queued again, would otherwise be cancelled without a handler. A job stopped otherwise, such as by a shutdown or a crash, leaves the import `running`, and its next run resumes it (FR-5). A job whose import already ended changes nothing, but removes the file of a completed or cancelled import, which its process may have left when it stopped between the end of the import and the removal.
- Job kinds: `import.native` processes a native push, next to `import.xml` ([XML import](ingestion-xml-import.md)) and `import.hae` ([Health Auto Export endpoint](ingestion-health-auto-export.md)). Every import job has the user of the import, the import id in its payload, `{"importId": 42}`, and the serial key of FR-4.
- An import has `id`, `origin`, `status`, `client` (the client metadata of FR-9, left out when there is none), `tokenName` (the name of the token used, left out when there is none), `createdAt`, `startedAt` and `finishedAt` (each left out until set), and `report`, never its token or its payload. The list gives the counters of `total` only, and `GET /api/v1/imports/{id}` the whole report. The report holds every counter of FR-3 in `total`, the counters per type key in `types`, those of workouts in `workouts`, leaving out the counters at zero in both, and the sample error messages in `errors`:

  ```json
  {
    "total": {"received": 1201, "inserted": 1180, "updated": 0, "duplicates": 18, "blocked": 0, "invalid": 2, "unsupported": 1, "suspicious": 0, "warnings": 1, "deleted": 1, "notFound": 0},
    "types": {
      "step_count": {"received": 1100, "inserted": 1082, "duplicates": 18},
      "heart_rate": {"received": 99, "inserted": 97, "invalid": 2, "deleted": 1},
      "HKDataTypeIdentifierElectrocardiogram": {"received": 1, "unsupported": 1}
    },
    "workouts": {"received": 1, "inserted": 1, "warnings": 1},
    "errors": ["record 17 (HKQuantityTypeIdentifierHeartRate): unit \"bpm\" is not a unit of count/min"]
  }
  ```

- Endpoints:
  - `GET /api/v1/imports` lists the imports of the user, newest first by creation, paginated as described in [Architecture](../architecture.md#pagination), for a session or a token with the `read` scope. The optional `status` filter takes one of the statuses of FR-9, and another value answers 422 `validation_failed`.
  - `GET /api/v1/imports/{id}` returns one import of the user, for a session or a token with the `ingest` or `read` scope, so that a client can poll the import it pushed. An import of another user answers 404 `not_found`, as an unknown id does.
  - `POST /api/v1/imports/{id}/cancel` cancels the job of a queued or running import of the user (FR-10, [Job worker](platform-jobs.md) FR-9) and answers 202 with the import, as `GET /api/v1/imports/{id}` returns it. The job of an import is the queued or running job of the serial key of its user whose payload names it. A running job sets the final status at the next batch boundary, and the import is still `running` in the response. An import that no handler can end, its job queued, waiting for a retry or gone, such as a job abandoned after its lost leases, ends `cancelled` in the request, which removes its inbox file once committed. It is for a session only: a token answers 401 `unauthorized`, as for every operation for sessions only. A finished import answers 409 `import_finished`.
- Audit events of imports ([Audit log](audit-log.md) FR-2), for every origin: the user of the import as actor, no subject, and the import id and origin in the payload, `{"id": 42, "origin": "native"}`, never values.
  - `import.started` is recorded in the transaction where a job moves the import from `queued` to `running`, so a resumed import records it once.
  - `import.completed`, `import.failed` and `import.cancelled` are recorded in the transaction that ends the import, with the counters of `total` in the payload, `{"id": 42, "origin": "native", "counters": {"received": 1201, ...}}`. They are recorded by the job, without the client of a request, except the cancellation of an import that no handler can end, such as a queued one, which ends in the request that cancels it and records its client.

## Edge cases

- Payload with mixed valid and invalid records: valid ones are stored, invalid ones counted.
- Unit mismatch for the same type across sources (kcal and kJ): both converted to canonical.
- Server restart during processing: the job resumes from the inbox file, at the position stored by the last committed batch (FR-5).
- Disk full while writing to the inbox: respond `507 Insufficient Storage` and leave no file, so the client retries later.
- A category value that the catalog does not list for a curated type, or a workout activity type that the activity type catalog does not list, as a new iOS release adds: kept, see [Storage and aggregates](data-storage-and-aggregates.md) FR-3 and FR-4.

## Acceptance criteria

- [ ] Ingesting the same payload twice produces zero inserts the second time.
- [ ] A crash in the middle of an import leaves a consistent database, and the job completes after restart.
- [ ] Block rules apply identically to XML, Health Auto Export and native imports.
- [ ] Native deletions remove the referenced records.

## Future ideas

- Pairing flow for the companion app.
- Server-sent events for live import progress instead of polling.
