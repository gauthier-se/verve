# 03 — Web self-service import

Status: ready-for-agent
Blocked by: 02 (authenticated web session; empty-state CTA lives on the seeded dashboard)

## Goal

Import an Apple Health `.zip` from the browser, end to end: upload, a background
import job with real two-phase progress, and a final report — then watch the
seeded "Aperçu" Panels fill with data.

## Scope

- **Upload endpoint** (`requireAuth`, Account-scoped): accept a `.zip`, reject
  early via `Content-Length` over a **configurable max (default 2 GiB)** with a 413
  before streaming, then **stream to `-data-dir/tmp/`** and hand the path to
  `applehealth.Import`. The folder / `export.xml` case is **not** accepted here
  (stays CLI).
- **Import job registry** (in memory, no new table): status
  `pending → running → done | failed`; **one in-flight import per Account** (a
  second upload is refused while one runs); temp file deleted on completion or
  failure; orphan temp files swept at startup.
- **Two-phase real progress.** Upload: bytes received / `Content-Length`. Import: a
  counting reader over the `export.xml` entry's declared `UncompressedSize64`
  (no pre-count pass). A status endpoint returns phase + percent, the final
  `Report` on success, and a human message on failure (invalid zip, no
  `export.xml`).
- **SPA.** An **Import page** (drop-zone + button) in the nav that polls progress
  and renders the report or the failure message; a **CTA in the empty state** on
  the dashboard, shown while the Account has no data, that points to the Import
  page.

## Out of scope

Browser folder / `export.xml` upload (CLI only). Resumable / chunked upload.
Persisted job state. Global rollback on failure.

## Acceptance

- [ ] A logged-in user drops or selects a `.zip` on the Import page, sees upload%
      then import%, then a report (added / skipped / unmapped); the "Aperçu" Panels
      then show data.
- [ ] A second concurrent import for the same Account is refused while one runs.
- [ ] An oversize upload is rejected with 413 **before** streaming; an invalid zip
      or missing `export.xml` ends the job as `failed` with a readable message.
- [ ] A mid-import failure leaves partial rows; re-uploading the **same** file
      completes idempotently (content key, ADR 0006) with no duplicates.
- [ ] The temp file is removed after completion or failure; orphan temp files are
      swept at startup.

## Refs

ADR 0016, ADR 0006, ADR 0004. CONTEXT.md: Import job, Import.
`internal/connector/applehealth/import.go`, `internal/api/server.go`,
`web/src/components/app-shell.tsx`, `web/src/lib/api.ts`.
