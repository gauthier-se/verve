Status: done

# 01: data, api: the Exclusion, the purge it performs, and its endpoints

## What

- **Migration `0013_exclusions.sql`**:
  ```sql
  -- An Exclusion is a standing refusal: no Connector may write what it names,
  -- and what was already stored under it was deleted when it was created. It is
  -- never a read-time predicate (see the ADR).
  CREATE TABLE exclusions (
      id         INTEGER PRIMARY KEY,
      account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
      metric     TEXT NOT NULL,
      -- Inclusive YYYY-MM-DD bounds, '' for unbounded. Empty string rather than
      -- NULL so the UNIQUE below actually catches a duplicate: SQLite treats
      -- NULLs as distinct, so two "the whole of body_fat_percentage" rows would
      -- both be admitted.
      starts_on  TEXT NOT NULL DEFAULT '',
      ends_on    TEXT NOT NULL DEFAULT '',
      purged     INTEGER NOT NULL DEFAULT 0,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
      CHECK (metric <> ''),
      CHECK (ends_on = '' OR starts_on = '' OR ends_on >= starts_on),
      UNIQUE (account_id, metric, starts_on, ends_on)
  ) STRICT;

  -- Every read is "this Account's whole set", loaded once per import and once
  -- per page; the index is for the account scope, not for a span lookup.
  CREATE INDEX exclusions_account ON exclusions (account_id, metric);
  ```
- **`internal/data/exclusion.go`**, following `annotation.go` and `pin.go`:
  - `Exclusion{ID, AccountID, Metric, StartsOn, EndsOn, Purged, CreatedAt}`.
  - `ListExclusions(ctx, accountID)`: the whole set, ordered by `metric, starts_on`.
  - `CreateExclusion(ctx, *Exclusion) (bool, error)`: **one transaction** that
    inserts the row and purges what it matches, writing the deleted count into
    `purged`. `ON CONFLICT (account_id, metric, starts_on, ends_on) DO NOTHING`,
    and on a suppressed insert it resolves the existing row and returns `false`
    with `purged` untouched, the same idempotence shape `InsertOne` already uses
    (`measurement.go:115`): saying the same thing twice is not an error and must
    not re-report a purge that already happened.
  - `DeleteExclusion(ctx, accountID, id)`: scoped by `account_id` in the `WHERE`,
    `ErrRecordNotFound` when nothing matched. It deletes the rule only. Nothing
    is restored, by design.
  - The purge predicate, in `MeasurementModel` so it sits with the other writer
    of that table, as `DeleteByMetric(ctx, tx, accountID, metric, startsOn, endsOn) (int64, error)`:
    ```sql
    DELETE FROM measurements
    WHERE account_id = ? AND metric = ?
      AND (? = '' OR date(start_at) >= ?)
      AND (? = '' OR date(start_at) <= ?)
    ```
    `date(start_at)` and not a string slice, because that is the expression every
    other day-grain predicate in `internal/query` uses (`query.go:73`), and the
    two must agree about which day a row belongs to. Bounds are **inclusive**,
    matching Annotations' `starts_on`/`ends_on` rather than the half-open
    resolved windows: both are days a person typed.
  - **No Source predicate.** The purge takes `Manual` rows too: "delete every
    body fat value" means every one, and the confirmation states the count first
    (issue 03). This is the deliberate counterpart of `Delete`'s guard at
    `measurement.go:148`, so the comment there needs a pointer to here, or the
    next reader will believe imported rows are still undeletable.
  - `CountByMetric(ctx, accountID, metric, startsOn, endsOn) (int64, error)`: the
    same predicate as a `COUNT(*)`, so the confirmation can name a number before
    anything is removed.
- **`internal/api/exclusionhandlers.go`**:
  - `GET /v1/exclusions` → `{"exclusions": [{"id": 3, "metric": "body_fat_percentage",
    "starts_on": "2026-01-01", "ends_on": "", "purged": 431,
    "created_at": "2026-08-24T09:11:00.000Z"}]}`.
  - `POST /v1/exclusions` taking `{metric, starts_on?, ends_on?}` → 201 with the
    row, or 200 with the existing row when it was already there. Validation
    through `validator.go`:
    - the slug exists in the Catalog, else 422;
    - `catalog.Lookup(slug).Nature == catalog.Imported`, else 422 naming the
      reason: a derived Metric owns no rows, its operands do;
    - `Aggregation != catalog.DurationByState`, else 422: `sleep` is stored as
      States and this milestone covers Measurements. Refusing loudly is the whole
      point, an accepted `sleep` exclusion that silently keeps importing sleep is
      the one outcome worse than not having the feature;
    - both dates `YYYY-MM-DD` when present, `ends_on >= starts_on` when both are.
  - `DELETE /v1/exclusions/{id}` → 204, 404 for an unknown or another Account's
    id, never a body that confirms it exists.
  - `GET /v1/measurements/count?metric=&from=&to=` **or** a `count` field on the
    Metric page's existing payload, whichever fits: issue 03 needs the number of
    rows a purge would remove, before the purge. Prefer extending the metric read
    the page already makes over a second round trip.
  - Register the block next to the Pin routes in `server.go`.
- **History**: a new `eventExclusion = "exclusion"` kind in `historyhandlers.go`,
  dated `created_at`, labelled with the Metric slug, carrying the span and
  `purged` count. Rank it after `eventPhase` and before `eventNote` in `kindRank`
  (`historyhandlers.go:385`): it is something the Account decided, not something
  it wrote. An Exclusion that is later deleted takes its event with it; the
  History reads current state, and there is no audit log here.
- **Tests** in `internal/api/exclusionhandlers_test.go`, mirroring
  `annotationhandlers_test.go`: create and list; the purge removing exactly the
  rows in span and none outside it, on both a bounded and an unbounded
  Exclusion; a purge crossing a `Manual` row and taking it; the day boundary
  (a row at 23:00 UTC on `ends_on` is in, one at 00:00 the day after is out); the
  same Exclusion posted twice returning 200 and not double-counting `purged`; a
  derived slug, a `sleep` slug, an unknown slug and an inverted span each 422;
  delete; an unknown id 404; cross-Account isolation on every verb, including
  that one Account's Exclusion purges nothing of another's.
  In `historyhandlers_test.go`: the event appears with its count and sorts on the
  right side of a same-day Phase and note.
- **Docs**: the new ADR (see the PRD) and the `Exclusion` entry in CONTEXT.md,
  in the cross-cutting section between **Import** and **Content key**. ADR 0022's
  "Considered Options" predicted this feature by name; the new ADR opens by
  saying so, and ADR 0022 gets a one-line pointer forward so its "delete any
  Measurement is silently broken" paragraph is not read as still current.

## Why here

The insert and the purge are one transaction because the half-states are both
bad in a way that is invisible afterwards: a rule with no purge is a delete that
did not happen, and a purge with no rule is data that comes back next month. The
feature's entire claim is that those two never separate.

The purge predicate lives in `MeasurementModel` next to `Delete` rather than in
the Exclusion model, because `Delete`'s comment currently states an invariant
this issue breaks. Putting the new statement anywhere else leaves that comment
true-looking and wrong, which is how the next reader learns the rule that no
longer holds.

The `sleep` refusal is worth its 422. Every other unsupported thing here is
either absent from the Catalog or obviously derived; `sleep` is a real, familiar,
Imported-looking slug whose rows are in another table, so accepting it would
produce a rule that reports success and does nothing, forever.

## Comments

Shipped. `internal/data/migrations/0013_exclusions.sql`, `internal/data/exclusion.go`,
`internal/api/exclusionhandlers.go`, the purge and its count in
`internal/data/measurement.go`, the `exclusion` event in
`internal/api/historyhandlers.go`, ADR 0033 and the CONTEXT.md entry. Tests:
`exclusionhandlers_test.go` and `TestHistoryCarriesTheExclusionEvent`. Whole suite green.

**Until issue 02 lands, an Exclusion purges and nothing more.** The Connector does not
read the table yet, so the next import restores every purged imported row: exactly the
failure ADR 0022 named. The endpoints are correct and the data is not yet safe from an
import, which is why these two issues are one feature and not two.

Four things came out of the implementation that the spec did not anticipate:

- **The preview is `GET /v1/exclusions/preview`**, not a count on `/v1/measurements` or
  a field on the Metric page's payload. It exists only to preview an Exclusion, and it
  has to validate exactly what the create path validates, or a preview that answers
  could be followed by a create that refuses. `/v1/measurements` is also the wrong host:
  it is the Manual-entry resource and refuses anything but `source=manual`.
- **The purge landed as an unexported `deleteMeasurementsInSpan(ctx, querier, ...)`**
  rather than a `MeasurementModel.DeleteByMetric` taking a transaction. A method hanging
  off a model whose `DB` it then ignores in favour of a passed querier misstates who owns
  the statement. It still lives beside `Delete`, which was the point, and shares
  `measurementSpanFilter` with the exported `CountInSpan` the preview calls, so the
  number shown and the number deleted are one predicate.
- **The History event needed two new typed fields**, `span_from`/`span_to`. Reusing
  `EndsOn` for the excluded span looked free and is wrong: `EndsOn` draws a band from
  `Date` across the axis, and an exclusion's date (when it was decided) and its span
  (what it is about) are different days by construction.
- **`validateExclusionMetric` duplicates the shape of `validateManualMetric`** rather
  than sharing it. The three branches are identical and the messages are the content:
  a Manual entry's derived-Metric refusal says "enter its operands instead", an
  Exclusion's says "exclude the operands it is computed from", and folding them into one
  helper would mean passing the messages in, which is the duplication with extra steps.

Two tests beyond the spec's list, both guarding invariants that are cheap to break
later: a rejected Exclusion leaves neither a rule nor a purge behind, and a Manual entry
is still accepted on an excluded Metric.
