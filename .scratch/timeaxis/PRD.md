# PRD — Time axis

## Goal

Give the Dashboard's **time axis** one home. Today "resolve a Dashboard's
temporal state to concrete windows and a bucket" is smeared across the client
(`resolveRange`, `autoBucket`) and three server files (`parseRange`,
`parseTimeRange`, `baselineWindow`, `validateRange`, `validateBaseline`), with
bucket words validated in three vocabularies. Consolidate all of it into one
server-side module and let the SPA forward tokens instead of computing dates.

Context and glossary: `CONTEXT.md` (**Time axis**, **Time range**, **Baseline**,
**Baseline rule**, **Ordinal alignment**) and ADR 0012, 0015.

## What this milestone does

- Adds `internal/timeaxis`: a pure, DB-free module owning temporal resolution —
  preset→window, baseline rule→window, span→bucket, `ALL_FLOOR`, and the closed
  sets. Two entries: `Validate(Tokens)` (PATCH dashboard) and
  `Resolve(Tokens, now)` (GET series).
- Moves range resolution **server-side**. `/v1/series`' temporal input becomes
  exactly the Dashboard's token set (`range`, `range_from/to`, `baseline`,
  `baseline_from/to`, optional `bucket`). Raw `from`/`to` and the `<N>[dwmy]`
  shorthand are removed.
- The SPA sheds all date math: `resolveRange`/`autoBucket`/`effectiveBucket`
  deleted; `panel-card`/`use-series` forward tokens.
- Pins the Go/SQL **bucket-boundary** contract with a direct agreement test and
  retires the prose that stood in for it (Candidate 3).

## What this milestone does NOT do

- **No behavioural change to graphs.** Same windows, same buckets, same overlay —
  only where the math lives changes.
- **No new presets, rules, or chart types.**
- **No back-compat layer.** Single binary + embedded SPA (ADR 0005) → server and
  client cut over atomically; no dual param support.
- **No repo-wide comment purge.** Parsimony applies to touched files only.

## Design invariants

- Resolution is **server-side, one module**; the client forwards tokens (extends
  ADR 0015's "computed once, tested once, provably consistent" from the baseline
  to the whole time axis).
- `Bucket` stays in `internal/query`; `timeaxis` imports it. `baselineWindow`
  (rule→window) moves to `timeaxis`; `alignOrdinal` (overlay of executed series)
  stays in `query`. `SeriesWithBaseline` becomes `Compare(current, baselineWindow)`
  — the engine does no rule date-math.
- `maxPoints` stays the `query` backstop; `timeaxis.autoBucket` picks a safe
  default, an override past the cap surfaces as `ErrRangeTooLarge` → 422.
- `range=all` resolves `Baseline=nil`; a baseline rule sent with `range=all` is a
  validation error (→ 422).
- The bucket boundary keeps two implementations (SQL groups, Go enumerates); a
  test proves they agree — the contract is executable, not narrated.

## Slices (issues)

1. **Bucket-boundary contract**: direct table-driven agreement test
   (`sqlExpr` label == `snap(t)` on real SQLite) + `starts`/`next` unit tests;
   retire the invariant prose. Pure addition, no runtime change. (Candidate 3)
2. **`internal/timeaxis` module**: `Tokens`/`Window`/`Resolved`,
   `Validate`/`Resolve`, `autoBucket`, `ALL_FLOOR`, closed sets, full unit tests.
   Not yet wired.
3. **Cutover** (atomic): `/v1/series` + PATCH use `timeaxis`; `query` drops
   `parseRange`/`parseTimeRange`, `baselineWindow` moves, `SeriesWithBaseline`→
   `Compare`; SPA forwards tokens and deletes `resolveRange`/`autoBucket`.

## House rule for this milestone

Document with parsimony: one-line godoc on exported symbols; comment only the
non-obvious (invariants, cross-module contracts, why); prefer an executable test
over a prose contract. Touched files only.
