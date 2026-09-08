# A read is a module, not a handler

## Context

Two endpoints had grown their own read engines inside `internal/api`.

`historyhandlers.go` and `ledgerhandlers.go` were 651 lines, and two of their
fifteen functions touched `w` or `r`. The other thirteen took a `ctx` and returned
a value. They materialised the bucket grid and named its gap runs (ADR 0032),
folded Phases onto that grid, derived the band's grain, folded a `Series` summary
into a per-day or per-night figure, and computed a week-over-week delta. None of
that is HTTP. All of it is the read path.

It had not been put there deliberately. Each piece arrived with the page that
needed it, and the API package was where the page was being written. The costs
compounded quietly:

- **The rules existed twice.** `historyBucket` was a verbatim copy of
  `timeaxis.autoBucket`, down to both thresholds, with a comment admitting it
  ("on the same thresholds the Dashboard's auto-bucket uses") and nothing
  compiling the two against each other. `foldPhases` was a second implementation
  of the bucket-folding `timeaxis.Resolved.Fold` already did. The RFC 3339 → day
  projection appeared four times in one file.
- **The reads had drifted off their tables.** `internal/data/history.go` held four
  functions with exactly one caller each, all of them this page. `ListImports` and
  `CountUnmapped` hung off `MeasurementModel` and read neither measurements.
  `DistinctMetrics` could only see measurements, so the Ledger patched sleep back
  in afterwards with a `withSleep` that knew sleep is stored as States (ADR 0027).
- **The test surface was wrong by an order of magnitude.** Asserting that a month
  of sleep divides by the Nights that hold data rather than by thirty cost a
  database migration, two argon2id hashes and a full HTTP round trip, because the
  only way to reach the arithmetic was through the router.

The engine also could not name its own inputs. `internal/timeaxis` imported
`internal/query` for the `Bucket` type alone, and that one edge meant the module
CONTEXT.md names as the owner of span→bucket could not define a bucket, while the
read engine could not accept a resolved `Window` in return — hence `Compare`
taking two loose `time.Time`, and hence the copy of `autoBucket`.

## Decision

**A read is a module.** Each read owns its own composition end to end and presents
one interface; `internal/api` decodes, calls it, and encodes the result.

**`Bucket` belongs to `internal/timeaxis`.** It moves there with its six pure
methods, and `autoBucket` becomes exported `AutoBucket`. The read engine keeps the
one genuinely SQL part as a private `bucketSQL`, so `timeaxis` stays pure and
DB-free as documented. `query.Request` and `query.Compare` then speak in
`timeaxis.Bucket` and `timeaxis.Window`.

**A read read lands on the module that owns the table it reads.** `Sources`,
`MetricsWithData` and the sleep-state probe read measurements and states, the two
families the engine already queries, so they are `query.Engine` methods. `Span`
unions measurements, states *and* sessions, so it hangs off `data.Models`: the one
read that is about every family at once. `ListImports`, `CountUnmapped`,
`InsertUnmappedBatch` and `RecordImport` move to a new `data.ImportModel`.

**`internal/query` gains `Band` and `Ledger`.** `Band` materialises the dense grid
and its gap runs and carries the `Grid` it drew, so anything folded onto that band
is placed against the keys the band actually used. `Ledger` folds every Metric over
the overview's fixed windows.

**`internal/history` is a new module** for the one read that genuinely spans
families: the band is aggregated Measurements or States, the events are Imports,
Phases, Exclusions and Annotations. It imports `query` and `data` and owns nothing
but that composition.

**The moved types keep their JSON tags.** `query.Series` and `query.Point` already
went straight onto the wire; the eight view structs in `internal/api` were a
mapping layer that existed for these two endpoints and no others.

## Why

- **The deletion test says it concentrates.** Delete the handlers and the
  complexity reappears in one module rather than across callers. That is the
  signal; a pass-through would merely have moved it.
- **The interface is the test surface.** Eleven of the fourteen tests now assert
  through the interface that owns the behaviour. The query half runs in 0.6s
  instead of through argon2id. The three that stay are the ones only the HTTP layer
  can answer: two auth checks and the unknown-metric 422.
- **One implementation cannot silently drift from another.** `historyBucket` and
  `autoBucket` could disagree and nothing would fail. After the move there is one
  `AutoBucket`, pinned by `TestAutoBucketThresholds`.
- **The caller should not know where a Metric is stored.** `MetricsWithData`
  answers the whole question, so `withSleep` and the API's `sleepSlug` are gone and
  the next State-backed Metric is a change in one place.
- **A module named after a table it does not read is how "where does this query
  live" stops having an answer.** Hence `ImportModel`.

## Considered Options

- **Move everything into `internal/query`, including the event feed.** One fewer
  package. Rejected: `query` would import `data` to reach Imports, Phases,
  Exclusions and Annotations — four tables it never aggregates — and the read engine
  has been deliberately independent of the row layer since it was written.
- **Leave the event feed in the handler and move only the band and the folds.** The
  smallest clean cut, and defensible. Rejected because it leaves the History page
  assembled in two places, which is the shape that produced the duplication in the
  first place.
- **Keep view structs in `internal/api` and map to them.** More orthodox layering.
  Rejected: it adds eight mappers the codebase does not have, and it would be
  inconsistent with `query.Series`, which goes straight onto the wire. Doing it
  properly would mean stripping the tags from `query` too and writing a view layer
  for every read — a larger change with no reader-visible benefit.
- **A leaf `internal/bucket` package** so `sqlExpr` need not be split off. Rejected:
  CONTEXT.md already names `timeaxis` as the owner of span→bucket, and the SQL half
  is the read engine's private business.
- **`internal/ledger` too, for symmetry.** Rejected: after the metadata reads moved,
  the Ledger needs nothing from `data`. A package that only namespaced `query` would
  be a name, not a module. The asymmetry with History follows from dependencies
  rather than from taste.

## Consequences

- The JSON both endpoints serve is byte-identical, verified field by field against
  `web/src/lib/types.ts`. The SPA is untouched.
- `internal/api` loses 589 lines across the two handler files (651 → 62) and the
  eight view structs that went with them.
- `internal/data/history.go` is deleted. `connector.Store` renames `RecordImport` to
  `Record` and is satisfied by `ImportModel`, so `data.ImportStore` and both
  connector test doubles gain a field.
- `Compare` now takes a `timeaxis.Window`. Every call site passed a resolved window
  it had unpacked by hand.
- `foldFigure` divides by `Series.Days` rather than by a constant the caller passes.
  The values are the same — the windows are 7 and 30 days — but the denominator now
  comes from the engine that computed it.
- A future read that spans families has a place to go and a precedent for how. A
  future read that does not should stay in `internal/query`.
