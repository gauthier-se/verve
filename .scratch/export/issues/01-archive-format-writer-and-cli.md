Status: done

# 01: data, cli: the Archive format, the writer that streams it, and `verve export`

## What

- **`internal/archive/archive.go`**, a new package holding the format and nothing
  about HTTP or the CLI:
  ```go
  // Schema is the Archive format version. It is written into every manifest and
  // checked on the way back in: a reader that does not know a version must refuse
  // the file rather than read the fields it recognizes (see the ADR).
  const Schema = 1

  // Manifest is what an Archive says about itself. It is the first entry in the
  // zip, so a reader can decide whether to continue after one small read.
  type Manifest struct {
      VerveArchive int      `json:"verve_archive"` // the marker Accepts looks for
      Schema       int      `json:"schema"`
      VerveVersion string   `json:"verve_version"`
      ExportedAt   string   `json:"exported_at"`
      Profile      *Profile `json:"profile,omitempty"`
      Counts       Counts   `json:"counts"`
      Warnings     []string `json:"warnings,omitempty"`
  }
  ```
  No email and no account id: an Archive identifies data, not a login. The
  `Profile` is `date_of_birth`, `biological_sex`, `blood_type`, the three
  `accounts` columns the basal estimates read (ADR 0023), carried so a transfer
  does not silently change a number, and applied by nobody (see issue 02).
- **The layout**, one entry per family plus the artifacts:
  ```
  manifest.json
  measurements.ndjson
  states.ndjson
  sessions.ndjson        each Session with its stats and Routes nested
  unmapped.ndjson
  imports.ndjson         provenance, never replayed
  artifacts/<sha256>.gpx
  ```
  A Session nests its stats and its Routes because `InsertWorkout` writes the
  three as one unit (`connector.Store`): the file has no reason to disagree with
  the store's own seam, and a flat `session_stats.ndjson` would need a foreign key
  the Archive has deliberately dropped along with the ids.
- **The row shapes carry the canonical fields and the content key, never the id.**
  `measurements.ndjson`:
  ```json
  {"metric":"heart_rate","value":58,"original_unit":"count/min",
   "start_at":"2026-03-02T06:14:00Z","end_at":"2026-03-02T06:14:00Z",
   "source":"Apple Watch","content_key":"3f2a…"}
  ```
  The key travels because it cannot be recomputed: `data.ContentKey` hashes the
  raw value string and raw unit the source wrote (`contentkey.go:34`), and the row
  holds the normalized value and `original_unit`. See the PRD's concept section;
  the ADR records it.
- **`archive.Source`**, the interface the writer pulls from, so the format is
  testable without a database and the CLI and the API share one implementation:
  ```go
  type Source interface {
      Profile(ctx context.Context) (*Profile, error)
      Measurements(ctx context.Context, after Cursor, limit int) ([]Measurement, Cursor, error)
      States(...)  // same shape
      Sessions(...)
      Unmapped(...)
      Imports(ctx context.Context) ([]Import, error) // small, unpaged
      OpenArtifact(name string) (io.ReadCloser, error)
  }
  ```
- **`archive.Write(ctx, w io.Writer, src Source) (Summary, error)`** streams a
  `zip.Writer` straight into `w`: manifest first, then one family at a time, a
  page at a time, encoding each row and letting the page go. Peak memory is one
  page, so the export runs where the import runs. Deflate for every entry,
  including the GPX, which is XML.
- **`internal/data/export.go`** implements `Source` over the models, with keyset
  paging per family, ordered to match each table's existing index so a five
  million row Account does not sort on disk:
  - measurements: `WHERE account_id = ? ORDER BY metric, start_at, content_key`,
    the prefix of `measurements_account_metric_start`;
  - states: `ORDER BY kind, start_at, content_key`, the prefix of
    `states_account_kind_start`;
  - sessions: `ORDER BY start_at, content_key`, `sessions_account_start`;
  - unmapped: `ORDER BY source_type, start_at, content_key`,
    `unmapped_account_type`.
  The cursor is the ordering tuple itself, and the seek predicate is the usual
  lexical `(a, b, c) > (?, ?, ?)` expansion. `content_key` is the tiebreak because
  `UNIQUE (account_id, content_key)` makes every tuple distinct, which is what
  makes the paging total and the output deterministic.
- **One snapshot.** The whole export runs inside a single read transaction through
  the existing seam (`data.Tx`, ADR 0038), so an import running at the same time
  cannot land half of itself in the middle of the file. Comment that this holds
  the WAL from checkpointing for the duration: that is the price of an Archive
  that is one consistent moment rather than several.
- **Sessions come out whole**: one query for the page of Sessions, then their
  stats and Routes by `session_id IN (…)` over the page, assembled in Go. Two
  round trips per page rather than per row.
- **Artifacts are written once.** Route rows name `artifact`, which is the sha256
  of the contents, so the writer keeps a seen-set and copies each file once. A
  Route whose artifact is missing from disk is still written as a row, and the
  manifest gains a warning naming it: the GPS track is gone either way, and an
  Archive that dropped the Route too would lose the fact that the workout had one.
- **`verve export --account=EMAIL FILE`** in `commands.go`, beside `import` and
  following it line for line: resolve the Account by email with the same
  `ErrRecordNotFound` message shape, refuse a missing `--account`, take exactly one
  path argument, and write to stdout when it is `-`, which is what makes
  `verve export --account=me - | age -r … > verve.age` the answer to the
  encryption this milestone does not implement. Add the line to `usage`.
- **`renderArchiveSummary(w, summary)`** beside `renderReport`, same column style:
  counts per family, the artifact count, the warnings, and the path written.
- **Tests** in `internal/archive/archive_test.go` and
  `internal/data/export_test.go`:
  - **determinism**: the same fixture Account written twice, with `ExportedAt`
    injected, is byte-identical, entry names and all;
  - **paging is total and non-overlapping**: a family of 2500 rows with a page size
    of 100 yields every row exactly once, in the declared order;
  - **a page boundary inside equal `(metric, start_at)`** still advances, which is
    the bug the `content_key` tiebreak exists to prevent;
  - **a Session round-trips whole**: its stats and both its Routes are nested under
    it and no stat is orphaned at a page edge;
  - **a missing artifact** produces a Route row and a manifest warning, not an
    error;
  - **cross-Account isolation**: an Archive of Account A contains no row of
    Account B, asserted per family, the same coverage every other model test has;
  - **the CLI**: `verve export` on a seeded database writes a readable zip and its
    summary, and an unknown `--account` fails with the message `import` uses.
- **Docs**: the new ADR (see the PRD) and the **Archive** entry in CONTEXT.md, in
  the cross-cutting section beside **Import** and **Content key**. The Content key
  entry gains one sentence: the key is carried by an Archive because its preimage
  includes what the source said and not only what Verve stored.

## Why here

The format is a package rather than a function on the API server because two
callers need it before this milestone ends and a third (a scheduled export in a
cron line) needs it the day after. The `Source` interface is what keeps the format
tests free of SQLite and keeps `internal/data` free of zip.

The ordering is chosen by the indexes and not by taste. `ORDER BY start_at` across
the whole account reads well in a spec and makes SQLite materialize and sort
millions of rows, on the machine least able to do it. Ordering by each table's own
index prefix costs nothing at read time and still gives a total, reproducible
order, which is all determinism needs.

The read transaction is the one thing in this issue that cannot be added later
without changing behaviour: without it, an Archive taken during an import is a
file that looks complete and is not, and nothing downstream can detect it.

## Comments

Shipped. `internal/archive/archive.go` (the format, the manifest, the streaming
writer), `internal/archive/source.go` (the one Source over the models),
`internal/data/export.go` (the keyset-paged family reads and the counts),
`verve export` in `cmd/verve/commands.go`, ADR 0039, the **Archive** entry in
CONTEXT.md with the amended **Content key** entry, and the pointer in ADR 0033.
Tests: `internal/archive/archive_test.go`, `internal/data/export_test.go`, and
three CLI tests. Whole suite green.

Five things came out of the implementation that the spec did not anticipate:

- **The manifest carries counts and not warnings.** The spec had both, and the two
  cannot coexist in an entry written first: counts are cheap up front (one COUNT
  per family, inside the same snapshot) but a missing artifact is only discovered
  at the end. Counts won because they are what a reader can check itself against,
  and the warnings went to the export summary, where the operator is. Nothing is
  lost: a reader that wants to know a Route's file is absent looks for the entry,
  which is the same fact without a second copy to disagree with.
- **`internal/data` does not import `internal/archive`.** The spec had `Source`
  returning archive types, which would have pointed the storage layer at a format.
  Inverted: the paged reads return `data` types, `archive` imports `data` and owns
  the conversion, exactly as `internal/connector` does on the way in. The types
  point down.
- **The page read closes its `*sql.Rows` before the stats and routes queries**,
  rather than deferring. The pool is one connection (ADR 0038), so an open cursor
  across another statement deadlocks instead of failing, which is a hang in a test
  rather than a red line.
- **`ExportPage` is the method name on three models**, not `ExportMeasurements`
  and friends: the receiver already says the family, and the two that are not a
  family method (`ExportUnmappedPage`, `ExportImports`) hang off `ImportModel`,
  which owns both tables.
- **The summary goes to stderr when the Archive goes to stdout.** `verve export
  --account=me -` exists so the file can be piped into `age`, and a report
  appended to the zip would corrupt exactly the case the dash was added for.
