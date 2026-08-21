Status: done

# 01: data, api: the Annotation model, its endpoints, and its bucket

## What

- **Migration `0012_annotations.sql`**:
  ```sql
  CREATE TABLE annotations (
      id          INTEGER PRIMARY KEY,
      account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
      label       TEXT NOT NULL,
      body        TEXT,
      starts_on   TEXT NOT NULL,
      ends_on     TEXT,
      created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
      updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
      -- A span ends on or after it starts; a single day has no end at all.
      CHECK (ends_on IS NULL OR ends_on >= starts_on),
      CHECK (label <> '')
  ) STRICT;

  -- Every read is "this Account's Annotations overlapping this window".
  CREATE INDEX annotations_account_start ON annotations (account_id, starts_on);
  ```
  Plus, in the same migration, the Dashboard's toggle, beside `baseline_rule`
  (0006) because both are properties of the shared time axis:
  ```sql
  ALTER TABLE dashboards ADD COLUMN annotations INTEGER NOT NULL DEFAULT 1;
  ```
- **`internal/data/annotation.go`**, following `pin.go` and `dashboard.go`:
  - `Annotation{ID, AccountID, Label, Body, StartsOn, EndsOn, CreatedAt, UpdatedAt}`.
  - `ListAnnotations(ctx, accountID, from, to string)`: every row **overlapping**
    the half-open window, i.e. `starts_on < to AND COALESCE(ends_on, starts_on) >= from`
   , a span that began before the range and is still running must appear, which a
    naive `BETWEEN` on `starts_on` would drop. Ordered by `starts_on, id`.
  - `CreateAnnotation`, `UpdateAnnotation` (partial, the `PATCH` shape
    `dashboard.go` already uses, touching `updated_at`), `DeleteAnnotation`.
    Update and delete are scoped by `account_id` in the `WHERE`, never by `id`
    alone.
  - `Dashboard.Annotations bool` read and written alongside the baseline fields.
- **Bucket folding in `internal/timeaxis`**: `func Bucket(w Window, b query.Bucket,
  day time.Time) (string, bool)`, the start date of the bucket containing `day`,
  and false when the day falls outside the window. This is the same boundary
  arithmetic `Resolve` already owns, exposed rather than duplicated, so the marker
  can never land between two bars. Table test over day/week/month, including a day
  before the window, a day on each boundary, and the last day of the last bucket.
- **`internal/api/annotationhandlers.go`**:
  - `GET /v1/annotations` taking the **same time-axis tokens** every other read
    takes (`range_preset`, `range_from`, `range_to`, `bucket`), resolved through
    `timeaxis.Resolve` exactly as `handlers.go:129` does. Response:
    ```json
    {"annotations": [
      {"id": 7, "label": "flu", "body": "...", "starts_on": "2026-03-12",
       "ends_on": "2026-03-19", "bucket": "2026-03-09", "end_bucket": "2026-03-16"}
    ]}
    ```
    `bucket` and `end_bucket` are the folded grid positions; `end_bucket` is
    omitted for a single day, and clamped to the window's first/last bucket for a
    span that overruns either end. No `baseline_*` token is read: markers describe
    the current range only (see the PRD).
  - `POST /v1/annotations`, `PATCH /v1/annotations/{id}`, `DELETE /v1/annotations/{id}`.
    Validation through the existing `validator.go`: `label` non-empty and at most
    120 characters, `starts_on`/`ends_on` `YYYY-MM-DD`, `ends_on >= starts_on`,
    `body` optional. Field-keyed 422 like every other handler; 404 for another
    Account's id, never 403 and never a body that confirms it exists.
  - Register the block next to the Pin routes.
  - `PATCH /v1/dashboards/{id}` accepts `annotations` (bool) and returns it on the
    Dashboard payload.
- **Tests** in `internal/api/annotationhandlers_test.go`, mirroring
  `pinhandlers_test.go`: create, list within a window, a span overlapping the
  window's start, a span overlapping its end, a day outside excluded, folding at
  `bucket=week` and `bucket=month`, patch, delete, an unknown id, an empty label
  and an inverted span rejected, and cross-Account isolation on every verb.
  In `dashboardhandlers_test.go`: the toggle defaults on, patches, and survives a
  patch that only touches the range.
- **Docs**: the new ADR (see the PRD's Docs section) and the `Annotation` entry in
  CONTEXT.md under the Dashboards section, after `Baseline rule` and before
  `Ordinal alignment`, with its `_Avoid_` list.

## Why here

The client could compute the marker's bucket itself: it holds the ordered grid
and could binary-search it. That is exactly the duplication `internal/timeaxis`
exists to prevent, one module owns bucket boundaries and the Go and SQL sides
are pinned to it by test (the Time axis milestone). A second, client-side
implementation of "which bucket holds this day" would be a third definition, in
a language where nothing tests it against the other two.

The overlap predicate in `ListAnnotations` is the one thing here that is easy to
get quietly wrong: "flu, 12-19 March" read on a range starting the 15th is
precisely the case that matters, and it is the case a `starts_on BETWEEN` drops.

The `annotations` column lands in the same migration as the table because a
toggle for a feature that does not exist yet is a column no writer sets.

## Comments

Shipped. `internal/data/migrations/0012_annotations.sql`,
`internal/data/annotation.go`, `internal/api/annotationhandlers.go`,
`Resolved.Fold` in `internal/timeaxis`, `query.Bucket.Start`, ADR 0030 and the
CONTEXT.md entry. Tests: `annotationhandlers_test.go`, `TestFoldOntoBucketGrid`,
and the two Dashboard toggle tests.

Three things came out of the implementation that the spec did not anticipate:

- **The folding helper landed as `Resolved.Fold(from, to)`**, not the planned
  `Bucket(w, b, day)`. Folding a whole span in one call is what both callers
  need, it puts the window clamping in the same place as the boundary lookup, and
  it reads off a `Resolved` the handler already holds. The boundary arithmetic
  itself is `query.Bucket.Start`, newly exported: `snap` stays where it is, so
  `TestBucketBoundaryGoSQLAgree` still pins the Go and SQL sides together.
- **`end_bucket` is emitted only when it differs from `bucket`**, rather than only
  for a span. A fortnight at the month bucket is one bucket wide and has no band
  to draw, so its presence is now exactly the client's marker-or-band test.
- **A Series is sparse**, which the spec missed: points come from a `GROUP BY`, so
  a bucket with no data is absent from the axis' categories, and Recharts places
  a `ReferenceLine` by matching the category exactly. A note on an empty bucket
  therefore has no category to sit on, and the case is not rare (the illness that
  flattens a curve is often the week that empties it). Issue 02 now carries the
  placement rule, and ADR 0030 records the limitation and names the real fix.

One convention differs from the issue text: an **empty string** clears a body or
an end day on `PATCH`, not JSON `null`. A `null` decodes to the same nil pointer
as an absent field, here as in every other handler in this API, so it cannot mean
"clear" without switching every input to `json.RawMessage`.
