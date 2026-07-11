# Web self-service import: zip upload, background job, idempotent re-import

## Context

ADR 0008 shipped import as a host-side CLI operation (`verve import
--account=… export.zip`) and deferred "self-service resumable web upload for the
747 MB export" to v1.x. This is that milestone: a non-developer must be able to
import an Apple Health export from the browser, with no shell access.

Several shapes had to be pinned: what the browser may upload, how a ~747 MB file
crosses HTTP without a multi-minute blocking request, whether progress is real,
where the in-flight run lives, and what happens when an import fails partway.

## Decision

The web accepts the **`.zip` only** — the exact artifact Apple's Health app
exports, drag-dropped or file-picked. The unzipped-folder / bare `export.xml`
case stays a **CLI** operation; `applehealth.Import` already opens a `.zip` and
resolves GPX routes inside the archive, so the web path adds no new parsing.

The upload is **streamed to a temp file** under `-data-dir/tmp/` (same volume as
`verve.db` and `artifacts/`), then handed to `Import(path)`. A configurable
**max upload size** (default 2 GiB) is rejected early via `Content-Length` (413)
before streaming. The temp file is deleted on completion or failure; orphans are
swept at startup.

Import runs as a **background job**, not inside the request. The `POST` returns a
job id immediately; the client polls a status endpoint. A job is
`pending → running → done | failed`, held in an **in-memory registry** (no new
table). **One in-flight import per Account**; a second upload is refused while
one runs.

Progress is a **real percentage in two phases**: upload bytes / `Content-Length`,
then XML bytes decoded / the `export.xml` entry's declared `UncompressedSize64`
(a counting reader wrapping the decoder input — no pre-count pass). On success
the job carries the existing `Report` (added / skipped / unmapped).

Failure surfaces as `failed` with a human message (invalid zip, no `export.xml`).
There is **no rollback**: streamed batches are already committed (ADR 0006), so a
mid-import failure leaves partial rows. Recovery is **re-upload** — the import is
idempotent by content key, so existing rows are skipped and the run completes.
Resumable upload (chunked/tus) is **not** built.

## Why

- **Zip-only.** It is the raw Apple artifact — no unzip step for the user — and
  the code already handles it end to end. Browser folder upload (`webkitdirectory`)
  ships thousands of route files plus a multi-GB decompressed `export.xml`: much
  more fragile for no user benefit. The folder case keeps a home in the CLI.
- **Temp file, then existing importer.** `Import` takes a path and streams in
  constant memory (ADR 0006); landing the upload on disk first reuses that whole
  engine unchanged. Same-volume temp keeps disk accounting and backup simple.
- **Background job, not a blocking request.** A 747 MB import runs for minutes;
  holding the connection invites browser/reverse-proxy timeouts, gives no
  progress, and loses everything on a dropped connection. A job id + poll is what
  a non-developer expects ("Importing… done").
- **In-memory registry.** Idempotent re-import makes crash recovery a re-upload,
  so persisting job state buys almost nothing against a new table and its
  lifecycle. Completed runs are already persisted by `RecordImport`.
- **Real two-phase percentage.** The zip central directory gives the decompressed
  size for free, so an honest bar costs a counting reader — worth it across a
  multi-minute wait.
- **No rollback, idempotent recovery.** Streaming commits batches by design;
  unwinding them would fight the memory model. Content-key idempotency already
  makes "run it again" the correct, safe recovery.

## Considered Options

- **Browser folder upload (`webkitdirectory`):** rejected — thousands of files +
  multi-GB `export.xml`, forces a manual unzip; the CLI covers the case.
- **Synchronous import in the request:** rejected — multi-minute request,
  timeouts, no progress, total loss on disconnect.
- **Job state persisted in SQLite:** rejected for now — a schema and
  interrupted/resume UX for a gain idempotent re-import already provides.
- **Resumable/chunked upload (tus):** deferred — real robustness for flaky links,
  but a large build; revisit if dropped uploads prove common.
- **Coarse spinner or line counter (no denominator):** rejected — the honest
  percentage is nearly free and matters most during the long wait.
- **Global rollback on failure:** rejected — fights streaming commits;
  idempotent re-import is the simpler, already-available recovery.

## Consequences

- A new upload handler streams to `-data-dir/tmp/`, enforces the size cap, and
  registers a job; a status endpoint reports phase, percent, and the final
  `Report` or error. Both sit behind `requireAuth` and are Account-scoped.
- An in-memory job registry tracks one run per Account with its lifecycle; startup
  sweeps orphan temp files.
- The SPA gains an **Import page** (drop-zone + button) in the nav and a
  **contextual CTA** in the empty state (see ADR 0018), polling for progress and
  rendering the report.
- `applehealth.Import` is reused unchanged; the counting-reader progress hook is
  the only addition to the import path.
- CLI import (including the folder / `export.xml` case) remains fully supported.
