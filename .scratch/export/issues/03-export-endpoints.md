Status: done
Blocked by: 01

# 03: api: the Archive as a stream, and a Series as a CSV

## What

- **`internal/api/exporthandlers.go`**, two handlers, both authenticated and both
  Account-scoped in the same breath as every other read (ADR 0007). They are the
  grown-up version of `handleDownloadRoute` (`sessionhandlers.go:311`), whose
  comment already states this milestone's thesis: "your data is yours" does not
  survive a server that only ever returns its own simplified version.
- **`GET /v1/export/archive`**:
  - `Content-Type: application/zip`, `Content-Disposition: attachment;
    filename="verve-2026-09-12.zip"`, dated to the day in the server's clock, the
    same shape the route download uses.
  - The body is `archive.Write` straight into the `ResponseWriter` over the
    `data.Source` issue 01 built, so nothing is buffered and no temporary file is
    written. No `Content-Length`: the size is not known when the headers go out,
    which is the trade this milestone accepts and the reason the web gets no
    progress bar (PRD).
  - `r.Context()` cancellation propagates, so closing the tab stops the read
    transaction rather than finishing a download nobody will receive.
  - **An error after the first byte cannot be a 422.** The status is already
    sent, so the handler logs it with the usual `serverErrorResponse` logger and
    returns **without** calling `Close` on the `zip.Writer`. An unfinalized zip has
    no central directory and every unzip tool refuses it loudly, which is the only
    honest signal left at that point. A comment saying exactly that, because the
    missing `defer zw.Close()` reads as a bug otherwise.
  - No job, no polling, no `imports`-style state (`importjob.go`): nothing is
    written, so there is nothing to resume or to report on.
- **`GET /v1/series.csv`**, taking exactly the parameters `/v1/series` takes and
  validating them with the same code: lift the metric and `timeaxis.Tokens`
  validation out of `handleSeries` (`handlers.go:103`) into one helper both call,
  rather than a second copy that drifts. It then calls `s.engine.Series` per
  Metric, so the file and the curve come from one read module (ADR 0037) and
  cannot disagree.
  - **`baseline_rule` is refused with a 422**, not ignored. A caller who asks for
    a comparison and receives one window would believe the file holds two. The
    message names the second download as the answer.
  - `Content-Type: text/csv; charset=utf-8`, `Content-Disposition` naming the
    Metric and the window: `verve-steps-2026-06-01-2026-08-31.csv`, and
    `verve-series-…csv` for a multi-Metric request.
  - RFC 4180, CRLF line endings, UTF-8, **no BOM**, header row:
    `bucket_start,bucket_end,metric,unit,aggregation,value,count`.
  - One row per bucket per Metric, Metrics in the order they were requested,
    buckets chronological. Numbers are plain dot decimals with no grouping, the
    rule `tsvNumber` already states for the clipboard path
    (`web/src/lib/clipboard.ts:5`).
  - **A gap is an empty `value` cell, never a zero** (ADR 0014, ADR 0032), and its
    `count` is `0`. This is the single most important line in the endpoint: a zero
    in a spreadsheet is a day you did not move, and a gap is a day the watch was
    on a charger.
  - `sleep` and any other `duration_by_state` Metric: one row per bucket per
    state, with the state in the `metric` column as `sleep.asleep_rem`, so the
    stack is legible in a flat file without inventing a second header shape. The
    night grain is whatever `Series` returned (ADR 0027).
  - `bucket_end` is the bucket's exclusive end as `timeaxis` resolved it, so a
    reader can reconstruct the grid without knowing Verve's rules.
- **Registration in `server.go`**, one block after the imports block
  (`server.go:159`), both behind `s.requireAuth`.
- **No new rate limiter.** The existing one is the login brute-force bucket
  (`ratelimit.go`), and an authenticated Account exporting its own data twice in a
  row is not an attack. Worth one sentence in the handler comment so the absence
  is a decision.
- **Tests** in `internal/api/exporthandlers_test.go`:
  - the Archive endpoint returns a zip that `zip.NewReader` opens, with the
    expected entries and the seeded row counts, and the `Content-Disposition`
    filename;
  - unauthenticated is 401 and another Account's data is absent from the body,
    the isolation assertion every handler test in this package makes;
  - the CSV's exact bytes for a small fixture, header row included, pinned as a
    golden string: this is the contract the file format is, and it is two windows
    wide, so pin it rather than describe it;
  - a gap renders as an empty cell and not `0`, asserted on the bytes;
  - a `sum` and a `latest` Metric in one request keep their own rows and their own
    aggregation column;
  - `baseline_rule=previous` is 422, an unknown Metric is 422, and a missing
    `metric` is 422, matching `/v1/series`;
  - a `duration_by_state` Metric emits its per-state rows.
- **`contract_test.go`** gains nothing: neither endpoint returns the JSON
  envelope, and the CSV's contract is the golden test above. Say so in a comment
  next to the contract list, so the omission is not read as an oversight.

## Why here

Both endpoints are in one file because they are one feature with two grains, and
splitting them across `handlers.go` and a `csv.go` would leave no place where the
choice between them is explained.

Lifting the parameter validation out of `handleSeries` rather than copying it is
the whole reason the CSV can be trusted: the file has to be the same read as the
chart, and a second validator is how "the same read" quietly becomes "almost the
same read" three commits later.

The unfinalized zip on a mid-stream error is the only part of this issue that
looks wrong in review and is right. HTTP gives one status per response; once the
export has begun there is no way to say "actually, 500" except by producing a
file that cannot be opened, which is better than a file that opens and is short.

## Comments

Shipped. `internal/api/exporthandlers.go` (both handlers, the CSV rows and the
filenames), `seriesParams` lifted out of `handleSeries` in `handlers.go`, the two
routes in `server.go`, and `Config.Version` wired from the CLI so a manifest can
say which build wrote it. Tests: `internal/api/exporthandlers_test.go`. Whole
suite green.

Three things came out of the implementation that the spec did not anticipate:

- **`Series` is sparse, so an empty day has no row at all.** The spec said a gap
  is an empty cell, which was written from ADR 0032's dense History band; the
  series endpoint the CSV mirrors simply omits a bucket with nothing in it. The
  file says what the read said. The empty-cell branch stays for a Point that does
  arrive marked as a gap, and is asserted directly on `csvRows` rather than
  through an endpoint that cannot produce one today: if a read path is ever made
  dense, the alternative is a zero written silently, which is the failure the rule
  exists to prevent.
- **The filename names the last day the file covers**, not the window's exclusive
  bound. `[From, To)` is right in a Window and wrong in a filename:
  `verve-steps-2026-06-01-2026-07-01.csv` for a June download reads as holding a
  day it does not.
- **`Config.Version` is new.** The manifest carries the build version and the
  server had none: the CLI knew it, the HTTP layer did not. Empty defaults to
  "dev", which is what a local `go build` already stamps.
