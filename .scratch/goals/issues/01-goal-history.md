Status: done

# 01: data, api: the Goal as a dated history

## What

- **Migration `0016_goals.sql`**, modelled on `0008_phases.sql`:

  ```sql
  CREATE TABLE goals (
      id          INTEGER PRIMARY KEY,
      account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
      metric      TEXT NOT NULL,
      direction   TEXT NOT NULL CHECK (direction IN ('at_least', 'at_most')),
      value       REAL NOT NULL,
      started_on  TEXT NOT NULL,   -- YYYY-MM-DD, inclusive
      ended_on    TEXT,            -- YYYY-MM-DD, exclusive; NULL while open
      created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
  ) STRICT;

  CREATE UNIQUE INDEX goals_one_open_per_metric
      ON goals (account_id, metric) WHERE ended_on IS NULL;
  CREATE INDEX goals_account_metric_started
      ON goals (account_id, metric, started_on);
  ```

  The header comment says why the bounds are dates and not instants (ADR 0044):
  a Goal judges days, and an instant would only raise the question of which Goal
  applies to the day it changed.

- **`internal/data/goal.go`**, a `GoalModel` mirroring `PhaseModel`:
  `Open` (closes the open Goal on that Metric at the new `started_on` and inserts,
  in one transaction through the transaction seam of ADR 0038), `ListByAccount`
  (optionally filtered by Metric), `InForce(accountID, metric, from, to)` (the
  segments overlapping a window, oldest first), `GetByID`, `Close`, `Delete`.

- **Validation, in the handler through the Validator:**
  - the Metric is in the Catalog and its aggregation is not `latest`, decided
    from the Catalog rule, not from a list;
  - `direction` is `at_least` or `at_most`;
  - `value` is finite; it may be negative (a derived Metric can be);
  - `started_on` is a date, not after today;
  - `started_on` is after the open Goal's `started_on` and not before the last
    closed Goal's `ended_on`, so segments never overlap. A refusal says to delete
    the entry being corrected.
  - closing sets `ended_on` to the given date, which must be after `started_on`
    and not after today.

- **Routes**, mirroring the Phases' (`server.go`), not under `/v1/metrics`
  (public Catalog):
  - `GET /v1/goals` (optional `?metric=`), newest first;
  - `POST /v1/goals`;
  - `PATCH /v1/goals/{id}` to close;
  - `DELETE /v1/goals/{id}`.

- **`Goal` in `web/src/lib/types.ts`**, pinned in `contract_test.go`.

## Tests

- opening a second Goal on a Metric closes the first at the new start date; a
  Goal on another Metric is untouched;
- the partial unique index refuses a second open Goal written behind the model's
  back;
- a `latest` Metric (`body_mass`) is refused, a derived one (`calorie_balance`)
  with a negative value is accepted, `sleep` and `training_time` are accepted;
- a start date before the open Goal's, or before the last closed Goal's end, is
  a 422; a future start date is a 422;
- another Account's Goal is a 404 on PATCH and DELETE;
- `InForce` returns exactly the segments overlapping a window, with an open
  Goal's end treated as unbounded.

## Why here

The history is the precondition of every count. Nothing downstream can be
honest about March if March's Goal can be overwritten, so the storage decision
comes first and alone.

## Comments

Shipped. `0016_goals.sql`, `internal/data/goal.go` with `goal_test.go`,
`internal/api/goalhandlers.go` with `goalhandlers_test.go` and its four routes,
`Goal` and `GoalDirection` in `types.ts` pinned by the contract test.

Three things differ from the spec:

- **The overlap rule lives in `GoalModel.Open`, inside the transaction**, not in
  the handler: a check made before the transaction could be raced. It is one
  bound, "the first day no earlier Goal holds" (a closed Goal's `ended_on`, an
  open one's `started_on + 1`), and violating it is `ErrGoalOverlap`, mapped to a
  422 on `started_on`.
- **The schema also CHECKs the direction and `ended_on > started_on`**, so a Goal
  that never held a day cannot be written behind the API's back. Closing a Goal
  on its own start date is a 422 that says to delete it instead.
- **`PATCH` takes an optional `ended_on`** (today by default), where the Phase's
  close takes none. A Goal judges days, and "I stopped aiming for this on the 3rd"
  is a fact the owner can know after the fact.

Goals are not in the Archive, like Phases and Annotations: ADR 0039's export is
the data, not the owner's arrangement of it.
