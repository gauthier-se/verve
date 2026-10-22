# Apple Health XML import

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Single binary](platform-single-binary.md), [User preferences and profile](user-preferences-and-profile.md)

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
- `Me` characteristics to prefill the profile.
- Resumable upload from the web app.
- CLI import.

### Out of scope

- `export_cda.xml` (clinical documents), electrocardiogram CSV files, audiograms, vision prescriptions, clinical records, activity summaries (rings). They are counted as unsupported.
- Importing exports from other platforms (Google Fit, Garmin).

## Functional requirements

- **FR-1**: Accepted inputs: `export.zip` (any folder name inside, typically `apple_health_export/`), or `export.xml` alone (then routes are not available and the report says so).
- **FR-2**: Upload uses the tus resumable upload protocol: chunked, resumable after network loss or page reload, with progress. Maximum size from `VERVE_MAX_UPLOAD_SIZE`.
- **FR-3**: Incomplete uploads are deleted after 24 hours.
- **FR-4**: Once the upload completes, an `import.xml` job is queued. The user can leave the page; progress is shown in the import banner and history.
- **FR-5**: The zip is read without extracting it to disk when possible. `export.xml` is parsed with a streaming XML tokenizer. The inline DTD at the top of the file is skipped, since some iOS versions produce a malformed DTD that breaks strict parsers.
- **FR-6**: Element handling:

  | Element | Handling |
  |---|---|
  | `Me` | Date of birth, biological sex, blood type and skin type are offered to prefill the profile (never overwriting values set by the user) |
  | `Record` (quantity) | Quantity sample: type, source name and version, device string, unit, creation, start and end dates, value, metadata entries |
  | `Record` (category) | Category sample, value mapped from the HealthKit category constant (for example sleep stages) |
  | `HeartRateVariabilityMetadataList` | Stored as sample metadata (instantaneous beats) |
  | `Correlation` | Inner records are imported as individual samples (blood pressure systolic and diastolic, food entries). Duplicates of top-level records are absorbed by deduplication |
  | `Workout` | Workout entity with activity type, duration, totals from attributes and `WorkoutStatistics`, `WorkoutEvent` items (pauses, laps, segments), metadata |
  | `WorkoutRoute` / `FileReference` | Route loaded from the referenced GPX file inside the zip |
  | `ActivitySummary`, `ClinicalRecord`, `Audiogram`, `VisionPrescription` | Counted as unsupported |

- **FR-7**: Progress is estimated from bytes read in the XML entry versus its uncompressed size.
- **FR-8**: Dates in the export have the format `yyyy-MM-dd HH:mm:ss Z`. The parser also tolerates the format variants produced by older iOS versions and documented in fixtures.
- **FR-9**: Percentages in the XML export (for example body fat, oxygen saturation) are fractions with unit `%`, converted accordingly.
- **FR-10**: CLI: `verve import xml --user <username> <path>` runs the same job in-process, prints progress and the final report, and takes the same per-user import lock as the server. It works while the server is running.
- **FR-11**: The final report includes per-type counts, the date range covered, and the number of routes attached to workouts.

## UX notes

- Import page with a drop zone and a short guide with screenshots: Health app, profile picture, "Export All Health Data", then share to Files or upload directly from Safari.
- Clear expectations: "An export of several years can take 10 to 30 minutes to process on a Raspberry Pi."
- The upload continues if the user navigates elsewhere in the app, and resumes after a reload when the same file is selected again.

## Data and API notes

- tus endpoint under `/api/v1/uploads/` (plain `net/http` handler, documented manually in the OpenAPI spec).
- Upload files in `/data/uploads`, deleted after successful import.

## Edge cases

- Export generated in a non-English iOS locale: identifiers and date formats are language-independent, but source names may contain non-ASCII characters and must be stored as UTF-8.
- Very large exports (over 5 GB uncompressed XML): streaming keeps memory bounded.
- Workouts referencing a GPX file missing from the zip: workout imported without route, counted in the report.
- Re-importing a newer export that overlaps an older one: only new samples are inserted.
- Samples with identical natural keys inside the same file: counted as duplicates.
- Corrupted zip or truncated XML: the import fails with a clear message; data committed before the failure stays.

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
