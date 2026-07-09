# 03 — Cutover: wire timeaxis, drop the twins

Status: blocked
Blocked by: 02

## Goal

Flip server and embedded SPA onto the token contract atomically (single binary,
ADR 0005 — no back-compat). After this, temporal resolution lives only in
`timeaxis`.

## Scope

- **API** (`internal/api`):
  - `/v1/series` temporal input = Dashboard tokens (`range`, `range_from/to`,
    `baseline`, `baseline_from/to`, optional `bucket`). Build `timeaxis.Tokens`,
    call `Resolve`, pass concrete windows + bucket to the engine. Map
    `timeaxis` validation errors → 422.
    Remove raw `from`/`to` and the `<N>[dwmy]` shorthand (`parseRange`,
    `parseTimeRange`).
  - PATCH dashboard + create/update panel validation call `timeaxis.Validate`;
    drop the duplicate `validateRange`/`validateBaseline`.
- **Query** (`internal/query`):
  - `baselineWindow` removed (now in `timeaxis`).
  - `SeriesWithBaseline` → `Compare(ctx, req, baselineWindow Window)`: runs both
    series, calls `alignOrdinal`; no rule date-math. `alignOrdinal` stays.
  - `maxPoints` backstop unchanged.
- **SPA** (`web/src`):
  - Delete `resolveRange`/`autoBucket`/`effectiveBucket`; keep `RANGE_PRESETS`.
  - `panel-card`/`use-series` send the dashboard tokens + panel bucket override;
    stop computing dates/bucket.

## Out of scope

No new behaviour. No new UI. Rendering, comparison control unchanged.

## Acceptance

- Graphs render identically to before (same windows/buckets/overlay) — verified
  end-to-end, not just unit tests.
- No `resolveRange`/`autoBucket` in the SPA; no `parseRange`/`parseTimeRange`/
  `baselineWindow` in the server.
- `main` is never in a broken intermediate state (single PR flips both sides).
- Existing api/query/web tests updated to the token contract and green.

## Refs

ADR 0005 (single binary), 0012, 0015. CONTEXT.md: Time axis.
