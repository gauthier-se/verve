# 04 — Query engine: the Manual overlay

Status: done
Blocked by: 01

## Goal

Make a Manual entry displace imported data **for its own day only**, so typing one
body mass corrects that day instead of erasing the rest of the chart.

## Why this issue exists

It was added mid-implementation. The original plan assumed Source priority resolved
per bucket, as `CONTEXT.md` claimed. It does not: `catalog.ResolveSource` elects one
winner for the whole range — its own comment says *"Whole-range only — per-bucket
resolution is deferred (ADR 0003)"* — and every read then filters `AND source = ?`
(`internal/query/query.go`, `aggregate`, `summarize`, `summaryMean`). Ranking
`Manual` first under that scheme would make one typed value the winner of the whole
window: on the reference Account, a single manual `body_mass` would reduce a
905-point chart to one point.

## Scope

- **`sourceFilter`** — a small value type in `internal/query` replacing the bare
  `source string` currently threaded through `aggregate`, `summarize`, `summaryMean`
  and the derived-operand path:
  ```go
  type sourceFilter struct {
      source    string // winning imported Source; "" when only Manual rows exist
      hasManual bool   // the Account has typed values for this Metric in the window
  }
  ```
  It renders the shared `WHERE` fragment and its args. Three cases:
  - **imported only** (`hasManual == false`) — emit exactly today's predicate,
    `source = ?`. Byte-identical SQL, so nothing existing can regress.
  - **Manual only** (`source == ""`) — `source = 'Manual'`.
  - **both** — the overlay:
    ```sql
    (source = ? OR source = 'Manual')
    AND (source = 'Manual' OR date(start_at) NOT IN (
          SELECT DISTINCT date(start_at) FROM measurements
          WHERE account_id = ? AND metric = ? AND source = 'Manual'
            AND start_at >= ? AND start_at < ?))
    ```
- **`resolveSource`** — split `Manual` out of the candidate list *before* calling
  `catalog.ResolveSource`, so it never competes as a Source, and report its presence
  separately. `ok` becomes "there is any row set at all": an imported winner, or
  Manual alone, or both.
- **`Series.Source`** — report the imported winner when there is one, else `Manual`.
  An active overlay does not change the reported Source; note this in the field's
  comment so it is not read as a bug later.
- Apply the filter uniformly: `aggregate` (all three rules), `summarize` (all three),
  `summaryMean`, `resolveOperand`, `summarizeDerived`. Aggregation semantics are
  **unchanged** — the overlay resolves the row set, then the existing rules run on
  top, which is precisely why day grain (not bucket grain) is the right choice.
- `catalog.SourceManual` from issue 01 is the only spelling of the literal.

## Out of scope

- Per-bucket resolution **between devices**. Still deferred (ADR 0003) and
  deliberately not reused: a device stream and a human correction want different
  mechanics.
- Any priority-table entry for `Manual`. It does not compete.
- Sub-day grain. Correcting a specific reading within a day is not a use case.

## Acceptance

- **No-overlay path is unchanged**: with no Manual rows, the generated SQL and every
  existing query test are untouched. This is the regression guard for the whole
  issue.
- A manual `body_mass` on day D, with `Zepp Life` readings on D-2…D+2, yields a
  5-point daily series where **only D** carries the manual value.
- With `sum` (e.g. a manual `steps` on D): day D totals the manual rows **only** — the
  device's rows for D are excluded, not added. A test pins that the day is not
  double-counted.
- With `average`: day D's mean and min/max band come from the manual rows only.
- The window **summary** agrees with the points it summarises — a `sum` summary over
  the window equals the sum of the per-day values shown, with the overlay applied
  once, not twice.
- Manual rows with no imported Source for the Metric produce a normal series.
- A derived Metric whose operand has a manual override picks it up through
  `resolveOperand` without extra wiring.
- Query timings on the reference dataset (≈310k `active_energy` rows) show no
  regression when no Manual rows exist.

## Refs

ADR 0022, ADR 0003 (whole-range priority, and why it is not reused here), ADR 0014
(derived operands), ADR 0019 (summary must match its points).
CONTEXT.md: **Manual overlay**, **Manual entry**, **Source priority**.
`internal/query/query.go` (`resolveSource`, `aggregate`, `summarize`, `summaryMean`,
`resolveOperand`, `summarizeDerived`), `internal/catalog/priority.go`.

## Comments

Implemented on branch `feat/manual-entry`.

- `sourceFilter{source, hasManual}` replaces the bare `source string` threaded through
  `aggregate`, `summarize`, `summaryMean`, `resolveOperand` and `summarizeDerived`. Its
  `where(req)` renders the predicate and args; `resolveSource` splits `Manual` out of
  the candidate list before `catalog.ResolveSource` elects a winner, so Manual never
  competes as a Source.
- **The no-overlay path emits the original predicate verbatim**, asserted directly by
  `TestNoManualRowsLeavesFilterUnchanged` rather than only implied by the suite passing.
  Manual-only is the same predicate pinned to `Manual`, so only the both-sources case
  has new SQL.
- The manual-days subquery is deliberately **not** range-filtered: a window boundary
  splitting a day would otherwise let the device's rows for that day survive next to the
  correction. Manual rows are few by nature, so scanning them all is cheaper than being
  subtly wrong.
- **Day grain paid off in `latest`.** That rule picks per bucket with a window function
  ordered by time; a manual row *earlier* in the day than the device's would lose under
  any time-ordered preference. Because the overlay removes the device row from the row
  set before aggregation, the rule needed no change at all.
  (`TestOverlayLatestPrefersManualWithinDay`)
- Verified against the real 310k-row dataset, not only fixtures: over a 1-year window on
  `active_energy` (231k rows) with one manual row, the overlay returns **360 days, same
  as the baseline** — no days dropped — with only the corrected day changed
  (1156.9 → 600.0). Cost ~145 ms → ~205 ms warm. Acceptable, and paid only for Metrics
  that actually have a Manual row; the Metrics people hand-correct are the sparse ones
  (`body_mass`, 905 rows), never the high-volume ones.
- 8 tests in `internal/query/manualoverlay_test.go` covering sum (no double-count),
  average (band follows the row set), latest, manual-only Metrics, a derived operand
  inheriting the overlay for free, and the unchanged-predicate guard.

### Incidental finding, recorded in `internal/catalog/priority.go`

Apple exports device names containing a **non-breaking space** (U+00A0, bytes `C2 A0`):
the stored Source is `Apple<U+00A0>Watch de Gauthier`, so an exact comparison against a
hand-typed `'Apple Watch de Gauthier'` silently matches **zero rows**. This cost a false
alarm during benchmarking. `ResolveSource`'s lowercase-substring matching ("watch") is
immune — that robustness is now documented as load-bearing rather than incidental, so
nobody "tightens" it into an exact match later.
