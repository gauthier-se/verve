Status: done

# 01: data, api: the Pin model and its endpoints

## What

- **Migration `0010_pins.sql`**:
  ```sql
  CREATE TABLE pins (
      id          INTEGER PRIMARY KEY,
      account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
      metric      TEXT NOT NULL,
      position    INTEGER NOT NULL DEFAULT 0,
      created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
  ) STRICT;

  -- One Pin per Metric per Account: pinning twice is a no-op, not a duplicate row.
  CREATE UNIQUE INDEX pins_account_metric ON pins (account_id, metric);
  -- List an Account's Pins in sidebar order.
  CREATE INDEX pins_account_position ON pins (account_id, position);
  ```
- **`internal/data/pin.go`**, following the shape of `dashboard.go`:
  - `Pin{ID, AccountID, Metric, Position}`.
  - `ListPins(ctx, accountID)` ordered by `position, id`.
  - `AddPin(ctx, accountID, metric)`: `position` = current max + 1, `INSERT ...
    ON CONFLICT DO NOTHING`, then return the existing or new row. Idempotent.
  - `DeletePin(ctx, accountID, metric)`: scoped by `account_id`, so one Account
    can never touch another's row.
- **`internal/api/pinhandlers.go`**:
  - `GET /v1/pins` → `[{metric, position}]`.
  - `POST /v1/pins` with `{"metric": "body_mass"}`. Validates the slug against
    the Catalog and returns the usual field error otherwise (same shape the
    other handlers use). Pinning an already-pinned Metric returns 200, not a
    conflict: the client's toggle should never have to distinguish.
  - `DELETE /v1/pins/{metric}`. 204 whether or not the row existed.
  - Register the three routes next to the Dashboard block in the router.
- **Tests** in `internal/api/pinhandlers_test.go`, mirroring
  `dashboardhandlers_test.go`: list empty, add, add twice (idempotent, one row),
  delete, delete absent, unknown slug rejected, and cross-Account isolation
  (Account A cannot list or delete Account B's Pins).
- **Docs**: the new ADR (see the PRD's Docs section) and the `Pin` entry in
  CONTEXT.md under the Dashboards section, with its `_Avoid_` list.

## Why here

A Pin is content, not a display preference: it follows the Account rather than
the browser, for the same reason a Dashboard does and the Appearance does not
(ADR 0024 rejected the server for Appearance because it is cosmetic, per-device
and would reintroduce the pre-paint flash; none of those apply here).

The unique index carries the idempotency rather than a read-then-write in the
handler, so a double click cannot race into two rows. `position` is written now
and exposed never: it exists so that adding drag-reordering later is a client
and handler change, not a migration.
