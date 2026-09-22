Status: done
Blocked by: none

# 01: data, now, api: Freshness at two grains, and the index it needs

## What

- **A read module, `internal/now`**, with an `Engine{Query, Models}` like
  `internal/day` and `internal/history` (ADR 0037). This issue gives it the
  Freshness half; `02` adds the Pin cards.

  ```go
  // Freshness is how old the Account's data is (ADR 0045): its last datum, and
  // each Source's last datum measured against it rather than against now.
  type Freshness struct {
      LastDay string         `json:"last_day,omitempty"` // empty for an Account with no data
      AgeDays int            `json:"age_days"`           // against the UTC today
      Sources []SourceFresh  `json:"sources"`            // active first, by lag; retired after
  }

  type SourceFresh struct {
      Source  string `json:"source"`
      LastDay string `json:"last_day"`
      LagDays int    `json:"lag_days"` // behind the Account's LastDay, 0 for the leader
      Retired bool   `json:"retired"`  // LagDays > 30
  }
  ```

- **The last datum per Source** comes from the read engine, next to
  `Engine.Sources` in `internal/query/metadata.go`, over measurements and states,
  `Manual` excluded, Sessions excluded. A day is the day the engine buckets the
  row in (UTC `date(start_at)`), so Freshness and a Panel never disagree about
  which day the data stops on.
- **Migration `0017_source_freshness.sql`**: an index on
  `measurements (account_id, source, start_at)` and on
  `states (account_id, source, start_at)`, so each Source's `MAX(start_at)` is
  answered from the index instead of scanning the Account. Check with
  `EXPLAIN QUERY PLAN` that the query uses it.
- **`GET /v1/now`**, authenticated, answering `{"freshness": ...}` for now.
  `now` is a parameter of the engine, not a `time.Now` call, so the age is pinned
  by test. Types pinned in the API contract test.

## Tests

- an Account with no data answers an empty `last_day`, no Sources, no error;
- the `Manual` Source never appears and never moves `last_day`;
- a Source 31 days behind the Account is retired, one 30 days behind is active;
- a Source replaced years ago is retired even when the Account itself is fresh;
- the Account's age counts from the UTC today;
- a sleep State's day is the one the engine would bucket it in.

## Why first

It is the half of the problem that is invisible today, and the data is one query
away. The Pin cards in `02` read their own ages and do not depend on it.

## Comments

Shipped. `Engine.SourceLastDays` in `internal/query/lastday.go`, `now.Engine.Freshness`
in `internal/now/now.go`, `GET /v1/now` in `nowhandlers.go`, `Now`, `Freshness` and
`SourceFreshness` in `types.ts` pinned by the contract test.

What differs from the spec:

- **Only measurements get the new index.** States are sleep and stand hours, a few
  thousand rows a year, and `states_account_kind_start` already narrows them to
  sleep, so a `GROUP BY source` over them is cheap. Measured on a real history
  (1.7 M measurements): the per-Source `GROUP BY` took 1.05 s, the loose index scan
  5 ms, for an index that grows the database by about 17%.
- **Stand States take no part.** No Metric reads them (ADR 0027), so a Source that
  only wrote stand hours is not data Now can point to.
- **A test pins the query plan**: it fails if the Source walk ever falls back to a
  scan of `measurements`.
