Status: done
Blocked by: 01

# 02: connector, cli: an import that refuses what the Account excluded

## What

- **`applehealth.Options`**, replacing the growing positional list. `Import` is
  already `(ctx, store, accountID, path, artifactsDir)` and
  `ImportWithProgress` adds a sixth; the Exclusions would be a seventh, and the
  two entry points differing only by one argument is already the smell.
  ```go
  // Options are the per-run inputs of an import beyond the source itself.
  type Options struct {
      ArtifactsDir string
      Progress     Progress    // nil on the CLI path
      Exclusions   Exclusions  // empty for an Account that has excluded nothing
  }

  func Import(ctx context.Context, store Store, accountID int64, path string, opts Options) (Report, error)
  ```
  Keep one exported entry point and delete `ImportWithProgress`: the package has
  two call sites, both in this repo (`cmd/verve/commands.go:177`,
  `internal/api/importjob.go:151`).
- **`Exclusions`, a resolved predicate, not a slice of rows.** The Connector must
  not import `internal/data`'s row type just to read three fields, and the check
  runs once per Record over a ~750 MB export, so it is built once at the top:
  ```go
  // Exclusions answers, per Record, whether the Account refused this Metric on
  // this day. Built once per import; the zero value excludes nothing.
  type Exclusions map[string][]Span // metric slug -> spans, '' bounds unbounded

  type Span struct{ StartsOn, EndsOn string } // inclusive YYYY-MM-DD, '' unbounded

  func (e Exclusions) Excludes(metric, startAt string) bool
  ```
  `startAt` is the normalized RFC 3339 UTC string `classifyRecord` produces, so
  the day is `startAt[:10]` and the comparison is lexical: a zero-padded
  `YYYY-MM-DD` prefix compares in the same order as a date, which is why the
  format was chosen (`import.go:488`). Guard a `startAt` shorter than 10
  characters, `normalizeTime` keeps an unparseable value verbatim.
  The map lookup misses immediately for every Metric the Account did not
  exclude, which is every Metric for almost every Account.
- **The check sits in the Record branch of `importStream`** (`import.go:340`),
  after `classifyRecord` returns a Measurement and before it is appended to the
  batch: the slug is what an Exclusion names, so there is nothing to check until
  the record has been classified. An excluded record is counted and dropped:
  ```go
  m, u, isMeasurement := classifyRecord(accountID, attrs)
  if isMeasurement {
      if opts.Exclusions.Excludes(m.Metric, m.StartAt) {
          c := report.PerMetric[m.Metric]
          c.Excluded++
          report.Excluded++
          report.PerMetric[m.Metric] = c
          continue
      }
      ...
  ```
  It does **not** fall through to the Unmapped bin. The bin exists so no source
  data is lost to a Catalog gap (ADR 0002); routing a deliberate refusal there
  would keep, row for row, exactly what was asked to be dropped.
  The State branch above it is untouched: this milestone excludes Measurements,
  and issue 01 refuses a `sleep` Exclusion at the door rather than pretending
  here.
- **`Report` reports it.** `Tally` gains `Excluded int`, `Report` gains
  `Excluded int`. Nothing in this feature may be silent: an import that drops
  data without saying how much is worse than the delete that did not stick.
- **`renderReport`** (`cmd/verve/commands.go:186`) prints the third column only
  for a Metric that has one, so an Account with no Exclusion sees the report it
  sees today, byte for byte:
  ```
  body_fat_percentage                       0 added         0 skipped      431 excluded
  ```
  plus a total line when `r.Excluded > 0`.
- **Both call sites load the set.**
  - CLI: `app.models.Exclusions.List(ctx, acc.ID)` before the call.
  - Web: `importRegistry.run` (`importjob.go:143`) loads it for the Account **at
    run time**, not at registry construction. The list is edited on the same page
    that starts the import, so reading it any earlier imports under a rule the
    owner has just changed. The registry holds a `Store`; it needs the exclusion
    reader too, either on that interface or as a small function field.
  - `importJobView.Report` gains `excluded`, alongside `added`/`skipped`/`unmapped`.
- **Tests** in `internal/connector/applehealth/import_test.go`: a fixture export
  imported with an unbounded Exclusion (the Metric is absent from the store and
  counted as excluded), with a bounded one (rows outside the span still land),
  with an Exclusion on a Metric the export does not contain (no effect, no
  panic), and with none at all (the existing assertions unchanged). A unit table
  for `Excludes`: unbounded, open start, open end, both bounds, a day on each
  bound, a malformed `startAt`. In `importhandlers_test.go`: the status payload
  carries `excluded`.

## Why here

The predicate is resolved into the Connector's own type rather than passed as
data rows for the reason every Connector-facing type in this package exists: the
next Connector gets exclusions for free by accepting the same `Options`, without
learning anything about how the Account stores them.

Loading the set at run time rather than at registry construction is a one-line
choice with a one-import consequence. The Import page is where Exclusions are
edited and where imports are started, so the interval between "I excluded body
fat" and "I dropped the zip" is seconds, and a cached set would spend that
interval being wrong.

## Comments

Shipped. `applehealth.Options` (and `ImportWithProgress` deleted), the check in
`importStream`, `Report.Excluded` and `Tally.Excluded`, the CLI's third column, both
call sites, and `excluded` on the import status payload. Tests: four in
`import_test.go`, two end-to-end in `importhandlers_test.go`, and the whole of
`internal/data/exclusion_test.go`. Suite green.

An Exclusion now purges *and* sticks: the feature works end to end from the API, and
issue 03 is what puts it on a screen.

Three things came out of the implementation that the spec did not anticipate:

- **The resolved predicate landed in `internal/data`, as `ExclusionSet`**, not as an
  `applehealth.Exclusions` built by each caller. The spec's reason for keeping it out
  of the Connector does not survive contact: `applehealth` already imports
  `internal/data` for every family type it writes, so the type would have been
  duplicated rather than avoided, and the two call sites would each have their own
  indexing loop. The decisive argument is the opposite one: `Excludes` is the Go twin
  of the SQL purge predicate, and the two belong in the same file, where a test can
  hold them to the same answer.
- **That test found a real divergence.** `normalizeTime` keeps an unparseable Apple
  timestamp verbatim, so a Record can reach the check with no readable day.
  SQLite's `date()` is NULL there and every comparison against NULL fails, so the
  purge leaves the row standing, while a lexical compare puts "not-a-date" after
  "2026-01-01" and would have refused it at the next import: excluded at import,
  present after a purge, for the same row. `Excludes` now parses the day before
  comparing, and takes an unbounded span as SQL does, which makes no comparison at
  all. `TestExclusionGoSQLAgree` carries the case.
- **The registry takes `data.Models` rather than `applehealth.Store`.** It needs two
  things from the models now, and the alternative was a second constructor parameter
  for a reader it uses once.

The CLI's third column and the report's `Excluded` line print only when non-zero, so
an Account with no Exclusion reads the report it has always read, byte for byte.
