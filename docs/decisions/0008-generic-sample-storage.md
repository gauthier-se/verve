# 0008. Generic sample storage with precomputed aggregates

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Apple Health has more than 100 quantity types and dozens of category types, and new ones appear with each iOS release. Dashboards will only cover a curated subset at first, but users should never have to re-import data when a new chart is added.

## Decision

- Store every quantity and category sample in one generic `samples` table keyed by a metric type from a catalog. Unknown types are added to the catalog automatically with their unit, so nothing received is dropped.
- Treat workouts as first-class entities with their own table and a compressed route per workout.
- Precompute hourly aggregates (UTC hours) and daily aggregates (user local days) after source selection, recomputed incrementally from dirty ranges.
- Exclude for now: ECG, clinical records (FHIR), audiograms, medications, state of mind. They are counted as unsupported in import reports.

## Consequences

- A single generic metric explorer can display any stored metric.
- Adding a dashboard never requires a migration or a re-import.
- The `samples` table becomes large. Indexes are designed around `(user_id, type_id, start_at)` and most reads hit aggregates instead.

## Alternatives considered

- **One table per metric**: strong typing, but a migration for every new type.
- **Store only curated metrics**: smaller, but data is lost and later features need re-imports.
