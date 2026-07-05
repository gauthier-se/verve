# 03 — State (sleep) + Session (workouts + routes) ingestion

Status: ready-for-agent
Blocked by: 02

## Goal

Complete ingestion coverage beyond scalars: interval states (sleep) and activity
sessions (workouts with GPX routes).

## Scope

- **State** (`internal/…`): parse `HKCategoryTypeIdentifierSleepAnalysis`
  (and `AppleStandHour`) → `states` table (owner, kind, state_value, start, end,
  source, content_key). Content key adapted (state_value included).
- **Session** (workouts): parse `Workout` elements → `sessions` table (owner,
  activity_type, start, end, duration, total_distance, total_energy, source) +
  associated `WorkoutStatistics`.
- **GPX route**: `WorkoutRoute`/`FileReference` → copy the `.gpx` into
  `VERVE_DATA_DIR/artifacts/`, store a row referencing the file (ADR 0004,
  artifacts as files). Dedup by content key / path.
- Extend the import report (State / Session / route counts).

## Out of scope

Workout UI (list, GPS map) → v1.x. ECG (deferred). Sleep `duration_by_state`
aggregation = query-side (slice 04).

## Acceptance

- Importing `sample_data`: sleep nights appear in `states`, the ~268 workouts in
  `sessions`, the `.gpx` files copied into `artifacts/` and referenced.
- Re-import is idempotent (States + Sessions + routes).

## Refs

ADR 0001 (State, Session), 0004 (file artifacts), 0006 (idempotency).
