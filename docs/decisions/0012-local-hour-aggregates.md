# 0012. Hourly aggregates aligned to local hours

- **Status:** Accepted
- **Date:** 2026-10-30
- **Supersedes:** the hourly boundaries of [ADR 0008](0008-generic-sample-storage.md)

## Context

ADR 0008 precomputes hourly aggregates on UTC hours and daily aggregates on the user's local days. Source selection works per hourly bucket, and daily rows are built from the selected hours. In a timezone with a 30 or 45 minute offset, such as India (UTC+5:30) or Nepal (UTC+5:45), a local day does not start on a UTC hour, so the first and last hourly buckets of a day straddle two days and daily totals cannot be exact.

## Decision

- Hourly buckets are aligned to the hours of the user's timezone, and stored as UTC instants. For whole-hour offsets they are identical to UTC hours.
- Changing the timezone recomputes the hourly and daily aggregates of all the user's types.
- The rest of ADR 0008 stands.

## Consequences

- Daily totals are exact in every timezone, and daylight saving days of 23 or 25 hours need no special case.
- Hourly rows depend on the user's timezone, so a timezone change costs a full recomputation of that user's aggregates.

## Alternatives considered

- **UTC hours, with daily aggregates computed from raw samples**: hourly rows would not depend on the timezone, but source selection would no longer match the buckets of a local day.
- **15-minute buckets**: they fit every offset, with four times as many rows.
