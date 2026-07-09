# 01 — Bucket boundary: prove the Go/SQL twin agrees

Status: done
Blocked by: —

## Goal

The bucket boundary is implemented twice — SQLite date math (`Bucket.sqlExpr`,
labels each row for `GROUP BY`) and Go calendar math (`Bucket.snap`/`starts`/
`next`, enumerates bucket starts for ordinal alignment). They must produce the
same boundary or a baseline bucket silently becomes a gap. Nothing proves it
today; the agreement lives in a prose comment. Make it executable.

## Scope

- **Agreement test** (`internal/query`): table-driven over a sweep of instants
  spanning day / ISO-week-Monday / month / year / leap-day (2024-02-29)
  boundaries. For each `t`, insert one measurement at `t` into an in-memory
  SQLite and assert the label `sqlExpr` emits equals `snap(t)` formatted — for
  Day, Week, Month. The test runs the **real SQL**, so it pins the twin.
- **Pure unit tests** for `starts(from, to)` (ordinal sequence, half-open,
  interior boundaries) and `next` stepping, no DB.
- **Retire the prose**: drop the invariant-narrating comments ("the Go twin of
  sqlExpr…", "Kept beside sqlExpr because the two must agree"); the test is now
  the contract. Trim remaining bucket-method comments to one line.

## Out of scope

No runtime code change to the bucket math itself (both impls stay). No `timeaxis`
module yet (02). No API/SPA change.

## Acceptance

- A failing implementation of either side (e.g. a wrong week offset) makes the
  agreement test fail.
- `starts`/`next` have direct tests that don't touch SQLite.
- No behavioural change; existing query tests still pass.
- Bucket-method comments reduced to one-line godoc; the two removed invariant
  comments are gone.

## Refs

ADR 0012 (server-side aggregation). CONTEXT.md: Bucket, Ordinal alignment.
`internal/query/query.go` (`sqlExpr`/`snap`/`starts`/`next`).
