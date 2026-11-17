# Apple Health XML import

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Single binary](platform-single-binary.md), [User preferences and profile](user-preferences-and-profile.md), [ADR 0015](../decisions/0015-handwritten-tus-server.md)

## Summary

Import the full Apple Health export (the `export.zip` produced by the Health app, or its `export.xml`) to backfill years of history. The file is uploaded from the web app with a resumable upload, or imported from the server with the CLI. Parsing is streaming, so multi-gigabyte exports never load into memory.

## User stories

- As a new user, I want to import all my past Health data in one go.
- As an iPhone user, I want to upload the export directly from Safari or the Files app.
- As an operator, I want to import a huge export from the command line when the browser is not practical.

## Scope

### In scope

- `export.zip` and bare `export.xml`.
- Records, correlations, workouts with statistics, events, metadata and GPX routes.
- `Me` characteristics to prefill the profile (M2).
- Resumable upload from the web app.
- CLI import.

### Out of scope

- Activity summaries (rings), clinical records, audiograms and vision prescriptions: elements of `export.xml` that are counted as unsupported under their element name (FR-6).
- Electrocardiograms: the `electrocardiograms/*.csv` entries of a zip, each counted once as unsupported under `HKDataTypeIdentifierElectrocardiogram` (FR-6).
- `export_cda.xml` (clinical documents): not read and counted nowhere, since it repeats documents, not records.
- Importing exports from other platforms (Google Fit, Garmin).

## Functional requirements

- **FR-1**: Accepted inputs: `export.zip` (any folder name inside, typically `apple_health_export/`), or `export.xml` alone (then routes are not available and the report says so, FR-6). The job reads the payload from disk, where it always is: the inbox file of an upload (FR-4) or the file of a CLI import (FR-10).
  - A payload that starts with a zip local file header (`PK\x03\x04`) is a zip, and one whose first byte other than a space, tab, carriage return or line feed is `<` is a bare `export.xml`. Any other payload fails the import at once, without retry, with the message `the file is not an Apple Health export`.
  - In a zip, the export is the first entry of the central directory named `export.xml` at the root or one folder deep, such as `apple_health_export/export.xml`. A zip without such an entry fails the same way.
- **FR-2**: Upload uses the tus resumable upload protocol 1.0.0, with the `creation`, `termination` and `expiration` extensions, under `/api/v1/uploads/` (Data and API notes): chunked, resumable after network loss or page reload, with progress.
  - It is for a session only: a token answers `401 unauthorized`. `POST`, `PATCH` and `DELETE` carry the CSRF header and pass the `Origin` check of every state-changing request ([Architecture](../architecture.md#security-baseline)). An upload of another user answers `404`, as an unknown one does.
  - Creation requires `Upload-Length`: a length above `VERVE_MAX_UPLOAD_SIZE` answers `413`, and `Upload-Defer-Length` is not offered. `Upload-Metadata` may carry `filename`, kept for display only.
  - `PATCH` writes at the stored offset, answers `409` when `Upload-Offset` differs from it, and never writes past `Upload-Length`. It syncs the file and stores the new offset before answering `204`. A full disk answers `507`, the offset staying at the last synced byte, so that the client resumes once space is freed.
  - Every write moves the expiry of the upload to 24 hours later, sent as `Upload-Expires` (FR-3).
  - `DELETE` removes the upload and its file.
  - The web app uploads in chunks of 8 MiB, so that a reverse proxy with a modest body limit lets them through. The [reverse proxy guide](../deployment/reverse-proxy.md) says that the proxy must accept bodies of that size.
- **FR-3**: Incomplete uploads expire 24 hours after their last write, not after their creation, so that a slow upload over a mobile link never expires while it progresses. An expired upload answers `404`, as an unknown one does, until `cleanup.uploads` removes it (Data and API notes).
- **FR-4**: The `PATCH` that writes the last byte of an upload completes it. The server syncs the file, renames it into the inbox (`/data/inbox`, on the same file system as `/data/uploads`), syncs both directories, then in one transaction deletes the upload and creates the import, origin `xml`, with that inbox file, the file name of the upload as the `name` of its `client` when the upload has one, and its job `import.xml` ([Ingestion pipeline](ingestion-pipeline.md) FR-9). The response carries the import id in the header `Verve-Import-Id`.
  - From then on the file has the lifecycle of every inbox file ([Ingestion pipeline](ingestion-pipeline.md), Data and API notes): deleted once the import completes or is cancelled, kept 7 days after a failure for retry or diagnosis.
  - A crash before the commit leaves an inbox file without import, which `cleanup.inbox` removes, and an upload whose file is gone, which answers `404` so that the client starts again.
  - The user can leave the page: progress is shown in the import banner and history.
- **FR-5**: The job reads the payload from disk, so the zip is never extracted: the entries of a zip are read through its central directory. `export.xml` is parsed with a streaming XML tokenizer.
  - The inline DTD at the top of the file is skipped by the reader itself, from `<!DOCTYPE` to the first `]>`, before the tokenizer reads these bytes, since some iOS versions produce a malformed DTD that breaks strict parsers. Positions count the skipped bytes.
  - The position of a record ([Ingestion pipeline](ingestion-pipeline.md) FR-5) is the byte offset, in the uncompressed `export.xml`, of the start tag of the record, and for a workout the offset of its end tag, since its inner records come before it, or of its start tag for a self-closing workout, which has no end tag. A resumed run reads the document again from its start and skips the records before the stored position.
  - A truncated XML document, a corrupted zip entry or malformed XML fails the import without retry, with a fixed message that names the cause and never a value: `export.xml is truncated`, `the zip file is corrupted` or `export.xml is not well-formed XML`. Corrupted compressed data can decompress to bytes that are not XML before the checksum of the entry tells the corruption: in a zip, a document that reads as truncated or malformed has the rest of its entry read, and fails as a corrupted zip when that read does. The batches committed before stay. A read error of the disk is retried as any job error ([Job worker](platform-jobs.md) FR-5), except in the CLI, whose failed attempt fails the job at once ([Job worker](platform-jobs.md) FR-7).
- **FR-6**: Element handling:

  | Element | Handling |
  |---|---|
  | `ExportDate` | Skipped |
  | `Me` | From M2, with the profile: date of birth, biological sex, blood type and skin type are offered to prefill the profile (never overwriting values set by the user). Skipped in M1 |
  | `Record` with a `unit` attribute | Quantity sample: type, value, unit, start and end dates, source, metadata |
  | `Record` without a `unit` attribute | Category sample, whose `value` is the category value identifier (for example `HKCategoryValueSleepAnalysisAsleepCore`), with type, start and end dates, source, metadata |
  | `MetadataEntry` | A metadata key of its record or workout, with its value as a string, as received, units included (`101.325 kPa`) |
  | `HeartRateVariabilityMetadataList` | Stored in the metadata of its record under the key `HeartRateVariabilityMetadataList`, as an array of beats with their attributes as received, `[{"bpm": "62", "time": "14:05:12,52"}]` |
  | `Correlation` | Its inner records are read as top-level records (blood pressure systolic and diastolic, food entries). The correlation itself, its attributes and its metadata included, is not stored and counts nowhere, since its inner records already hold the values |
  | `Workout` | Workout, see below |
  | `Record` inside `Workout` | Read as a top-level record. It also exists at top level, and deduplication absorbs it |
  | `WorkoutRoute` / `FileReference` | Route loaded from the referenced GPX file inside the zip, see below |
  | `ActivitySummary`, `ClinicalRecord`, `Audiogram`, `VisionPrescription` | Each counts as `received` and `unsupported` under its element name as the type key |

  Records:
  - A quantity sample of a curated category type, or a category sample of a curated quantity type, is invalid, as in the native format ([Ingestion pipeline](ingestion-pipeline.md) FR-7).
  - `sourceName` is the name of the source. The `device` string, such as `<<HKDevice: 0x141b957d8>, name:Apple Watch, manufacturer:Apple Inc., model:Watch, hardware:Watch7,3, software:26.0>`, gives the device fields of the source: `name`, `manufacturer`, `model`, `hardware` as the hardware version and `software` as the software version, a field that the string lacks being left out, and its other fields ignored. `sourceVersion` and `creationDate` are not stored, and a record has no bundle id.
  - The beats of a `HeartRateVariabilityMetadataList` keep their `time` as received: a time of day whose format follows the locale of the export, for example `14:05:12,52`. Interpreting it belongs to the [Heart page](page-heart.md) of M4, the first reader of beats.

  Workouts, whose source is read as for a record:
  - The duration comes from `duration` and `durationUnit`.
  - The totals come from its `WorkoutStatistics`: the `sum` of `HKQuantityTypeIdentifierActiveEnergyBurned` as the active energy, its sum with the `sum` of `HKQuantityTypeIdentifierBasalEnergyBurned` as the total energy when both exist, the `sum` of the first statistic whose type starts with `HKQuantityTypeIdentifierDistance` as the distance, and the `average` and `maximum` of `HKQuantityTypeIdentifierHeartRate` as the average and maximum heart rates. Without such statistics, the attributes `totalDistance` and `totalEnergyBurned` of older exports, with their units, give the distance and the active energy.
  - The elevations come from the metadata `HKElevationAscended` and `HKElevationDescended`, and the indoor flag from `HKIndoorWorkout` (`1` or `0`). These entries also stay in its metadata.
  - Every statistic, totals included, and every zone group are stored in its metadata under the keys `WorkoutStatistics` and `WorkoutZoneGroup`, as arrays of objects holding their attributes as received, a zone group with its `WorkoutZone` items under `zones`: `[{"type": "HKQuantityTypeIdentifierHeartRate", "source": "system", "unit": "count/min", "zones": [{"maximum": "120", "duration": "2.5000", "durationUnit": "min"}]}]`. Verve computes its own zones (see [Workouts](page-workouts.md) FR-4).
  - A `WorkoutEvent` starts at `date` and ends `duration` later, in `durationUnit`, or at `date` without duration.
  - A workout that the adapter cannot read is invalid, with a sample error message that names the attribute or the metadata key, never its value: one without `workoutActivityType`, or whose dates, duration, totals used, elevations or events are not dates, numbers or quantities, or whose event duration is not in a unit of time, or whose basal energy is not in a unit of energy.

  Routes:
  - A `FileReference` path resolves against the folder of `export.xml` in the zip: `/workout-routes/route_2025-12-30_7.00am.gpx` next to `apple_health_export/export.xml` is `apple_health_export/workout-routes/route_2025-12-30_7.00am.gpx`.
  - The GPX points are read in time order. The adapter drops the points outside the time range of the workout, and the workout counts under `warnings` with a sample error message, instead of being refused by the pipeline.
  - A workout whose GPX entry is missing, is not well-formed XML or holds a point whose `lat`, `lon`, `time`, `ele`, `speed`, `course`, `hAcc` or `vAcc` cannot be read, or whose export is a bare `export.xml`, is stored without route and counts under `warnings` with a sample error message. A bare `export.xml` also adds the sample error message `routes are not available in a bare export.xml`, once, to the message of its first such workout.
  - A corrupted GPX entry fails the import as a corrupted zip does (FR-5).

  Other entries of a zip:
  - Each `electrocardiograms/*.csv` entry of the folder of `export.xml` counts once as `received` and `unsupported` under `HKDataTypeIdentifierElectrocardiogram`, at positions after the size of `export.xml`, in central directory order.
  - `export_cda.xml` is not read and counts nowhere, since it repeats documents, not records.

- **FR-7**: Progress is the position over the uncompressed size of `export.xml`, read from the central directory for a zip, since data descriptors leave the local headers without size, and from the file size for a bare `export.xml`. It is at most 1, since the electrocardiograms of a zip come after `export.xml`, and 1 once the import completed, so that the end tags after the last record count. The job stores the size once it opens the export, and the import exposes `progress` ([Ingestion pipeline](ingestion-pipeline.md), Data and API notes). The import banner and the CLI show it as a percentage.
- **FR-8**: Dates in the export have the format `yyyy-MM-dd HH:mm:ss Z`, such as `2025-12-30 07:00:00 +0100`. The parser also tolerates an offset written `+01:00`, an offset `Z`, fractional seconds and a one-digit hour. Any other date makes its record invalid, with a sample error message that names the attribute. A variant that a real export shows later gets its own issue (#92).
  - Every date carries the offset of the day of the export, even a date from another season: the instant is right, but the offset says nothing about the sample. The stored offset is still the one of the date. Aggregates use the timezone of the user ([ADR 0012](../decisions/0012-local-hour-aggregates.md)), and `HKTimeZone` stays in the metadata for the pages of M2 that need the local time of a workout.
- **FR-9**: Percentages in the XML export (for example body fat, oxygen saturation) are fractions with unit `%`, converted accordingly.
- **FR-10**: CLI: `verve import xml --user <username> <path>` runs the same job in-process and takes the same per-user import lock as the server (see [Job worker](platform-jobs.md) FR-7). It works while the server is running, and on a data directory that the server never started on: it first synchronizes the metric types of the catalog of its own binary, as the server does at each start (see [Storage and aggregates](data-storage-and-aggregates.md)). Next to a running server, the command is the binary of that server, so that both use the same catalog.
  - The username is folded as at login ([ADR 0013](../decisions/0013-case-folded-logins.md)). A relative path is made absolute against the working directory of the command.
  - An unknown user, a disabled user or one being deleted, and a path that cannot be opened as a file, exit `1` with the reason on stderr, and create no import.
  - While another import of the user is queued or runs, the command says so on stderr and waits (see [Job worker](platform-jobs.md) FR-7). The import is created with its job once the wait ends, so that a command stopped while it waits leaves no import.
  - The command registers only the `import.xml` job: the aggregates that the import changes are recomputed by the worker of the server, or at its next start.
  - The import has the origin `cli` and no inbox file. The absolute path of the file is in the payload of its job, `{"importId": 42, "path": "/data/export.zip"}`, and the file is read in place, never copied, moved or deleted. A server that resumes the job of a command that died reads the same path, and fails the import without retry when the file is gone, as for a missing inbox file.
  - The command prints its progress to stderr as a percentage, at most every 5 seconds, and once the import ends, its status, the counters of `total`, the period and the routes of the report to stdout. It exits `0` when the import completes and `1` otherwise, a failed import printing the message of its failure to stderr. Interrupting it cancels the import, which ends `cancelled` ([Job worker](platform-jobs.md) FR-7).
  - In Docker: `docker exec verve verve import xml --user alice /data/export.zip`, the file being copied into the data volume first.
- **FR-11**: The final report includes per-type counts, the date range covered (`period`) and the number of routes that the import stored with workouts (`routes`), as for every origin ([Ingestion pipeline](ingestion-pipeline.md) FR-3).

## UX notes

- Upload page `/imports/xml`, linked from the imports page and from its empty state, with a drop zone and a short guide of numbered text steps: Health app, profile picture, "Export All Health Data", then share to Files or upload directly from Safari. The guide has no screenshots, which would hold Apple's interface and age with each iOS release.
- Clear expectations: "An export of several years can take 10 to 30 minutes to process on a Raspberry Pi."
- The upload is held by the app shell, not the page, so that it continues while the user navigates elsewhere in the app. While it runs, the import banner shows its percentage with a link to the upload page.
- Selecting the same file again after a reload resumes its upload: the browser keeps the upload address of each file in local storage.
- Once the upload completes, the page follows the import named by `Verve-Import-Id` and shows its progress, then its report: counters per type, period and routes.

## Data and API notes

- tus endpoint under `/api/v1/uploads/`, written by hand as a plain `net/http` handler ([ADR 0004](../decisions/0004-huma-code-first-openapi.md), [ADR 0015](../decisions/0015-handwritten-tus-server.md)) and documented manually in the OpenAPI spec. The web app calls it through `tus-js-client`, the one exception to the generated API client ([Architecture](../architecture.md#frontend)).
- Table `uploads`: id, user, file name, length, offset, created at, expires at. It holds incomplete uploads only, their files in `/data/uploads` under a random name.
- The recurring job `cleanup.uploads` runs hourly and removes the expired uploads with their files, and the files of `/data/uploads` without upload that are older than one hour.
- Account deletion removes the uploads of the user and their files ([Accounts and administration](accounts-and-administration.md), Data and API notes).
- An upload records no audit event: the import it becomes does ([Ingestion pipeline](ingestion-pipeline.md), Data and API notes).
- A completed upload is an inbox file of `/data/inbox` (FR-4): imported files are not kept in `/data/uploads`.
- Tests use a synthetic export with the shape of a real one and invented data: `internal/synthexport` generates it, its `testdata/` keeps the fixture unpacked, and `tools/synthexport` writes an export of any size.

## Edge cases

- Export generated in a non-English iOS locale: identifiers and date formats are language-independent, but source names may contain non-ASCII characters and must be stored as UTF-8.
- Very large exports (over 5 GB uncompressed XML): streaming keeps memory bounded.
- Workouts referencing a GPX file missing from the zip: workout imported without route, counted under `warnings` with a sample error message (FR-6).
- Route points outside the time range of their workout: dropped, the workout counted under `warnings` (FR-6).
- Re-importing a newer export that overlaps an older one: only new samples are inserted.
- Samples with identical natural keys inside the same file: counted as duplicates.
- Corrupted zip or truncated XML: the import fails without retry with a fixed message; data committed before the failure stays (FR-5).
- A file that is neither a zip nor an XML document, or a zip without `export.xml`: the import fails at once with `the file is not an Apple Health export` (FR-1).
- Elements under the root come in no guaranteed order: heart rate variability records follow `ActivitySummary`, and each type of record is one block, which is not sorted by date across sources.
- Zip entries use data descriptors: their local headers hold no size, so the uncompressed size that progress needs (FR-7) comes from the central directory.

## Acceptance criteria

- [ ] A 2 GB export imports successfully on a Raspberry Pi 4 with peak memory under 300 MB.
- [ ] An interrupted upload resumes from where it stopped.
- [ ] Re-importing the same export inserts nothing new.
- [ ] Workouts with GPX routes display their map after import.
- [ ] The CLI import works while the server is running.

## Future ideas

- Activity summaries (rings) import.
- ECG, audiogram and clinical records import.
- Watched folder import (rejected for now because file ownership is ambiguous with multiple users).
