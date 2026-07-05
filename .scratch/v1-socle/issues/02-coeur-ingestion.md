# 02 — Ingestion core: Catalog + parser + Measurements + dedup + report

Status: ready-for-agent
Blocked by: 01

## Goal

The slice that proves the whole pipeline: `verve import --account=… export.zip`
reads the Apple export in streaming, maps to the Catalog, normalizes units,
writes deduplicated **Measurements** to SQLite, and prints a report.

## Scope

- **Catalog** (`internal/catalog`): canonical Metrics as declarative data
  (neutral slug, canonical unit, aggregation rule, nature=imported). Seed
  **broad**: most of the ~90 Apple `HKQuantityTypeIdentifier*` types (at least
  every scalar present in `sample_data`). Per-Metric unit conversion.
- **Apple Health Connector** (`internal/connector/applehealth`):
  - Unzip / read `export.xml` in **streaming** (`xml.Decoder`, constant memory —
    the file is ~750 MB).
  - For each scalar `Record`: resolve the Metric via the mapping; unknown type →
    **Unmapped bin** (kept, never discarded); normalize the value to the
    canonical unit (original unit kept as metadata).
  - **Content key** = hash of `(metric, source, startDate, endDate, value,
    unit)`; `creationDate` **excluded**. Skip if already present → idempotent
    import.
  - Everything scoped to the `Account` passed via `--account`.
- `measurements` table (owner=account, metric, value, original unit, start, end,
  source, content_key unique per account) + time indexes useful for buckets.
- `imports` table: timestamp, file, added/skipped/unmapped counts.
- CLI output: readable report (per Metric: n added / n skipped) + total.

## Out of scope

State (sleep), Sessions (workouts), nutrition "meal grouping" (nutrients are
ingested as Measurements; the meal link → a later slice), UI, API.

## Acceptance

- Importing `sample_data/export.xml`: memory stays bounded, completes.
- Re-importing the **same** file: ~0 added, everything "skipped" (idempotency).
- A `SELECT` shows `heart_rate`, `steps`, `dietary_energy`… with canonical units
  and the correct `owner`.
- Non-seeded types land in the Unmapped bin, counted in the report.

## Refs

ADR 0001, 0002, 0003 (source priority: storage only here), 0006, 0011.
