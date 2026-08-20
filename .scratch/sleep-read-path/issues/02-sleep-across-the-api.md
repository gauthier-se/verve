Status: done

# 02: data, api: sleep in the Ledger, and the two guards

Depends on 01.

## What

- **`internal/data/state.go`** gains one read, beside `InsertStateBatch`:
  `HasStates(ctx, accountID, kind string) (bool, error)` — a
  `SELECT EXISTS(SELECT 1 FROM states WHERE account_id = ? AND kind = ?)`,
  served by the existing `states_account_kind_start` index. Enough for the
  Ledger's question ("does this Account have sleep at all"), and deliberately
  not a `DistinctKinds`: there are two kinds, one of which this milestone does
  not read.

- **`internal/api/ledgerhandlers.go`**: after the `DistinctMetrics` slugs,
  append `"sleep"` when `HasStates(accountID, "sleep")`. The row then folds
  through the same `ledgerRow` path as every other Metric — no branch — because
  `Compare` and `Series` already serve it after 01.

  One branch is needed inside `ledgerRow`: the week/month figures divide a
  window fold by its day count for a `sum` Metric, so that steps read per day.
  A `duration_by_state` Metric divides by `Series.Nights` instead. Dividing
  sleep by 30 when the Watch was off for nine of them would report a lie with
  the confidence of a computed number.

  Also update the doc comment: "Metrics the engine cannot serve yet (e.g.
  duration_by_state) are skipped" stops being true here.

- **Guard 1, manual entry.** `POST /v1/measurements`
  (`internal/api/measurementhandlers.go`) rejects a Metric whose aggregation is
  `duration_by_state`, with a field error saying sleep cannot be typed. Without
  it the row is accepted, stored in `measurements`, and then never read by
  anything — the worst failure available, because it looks like it worked.

- **Guard 2, Formula operands.** In the Catalog's build-time validation, beside
  the existing Formula checks, assert no operand resolves to a
  `duration_by_state` Metric. Nothing violates it today; it is a fence around
  the corner where `resolveOperand` would happily fold time asleep into a
  ratio and produce a number with no meaning.

- **Tests**:
  - `ledgerhandlers_test.go`: an Account with sleep states gets a `sleep` row
    whose week figure is the per-night mean (seed a window where nights with
    data ≠ days in window, so a per-day divisor fails the test); an Account
    with none gets no such row and no error.
  - `measurementhandlers_test.go`: `POST` with `"metric":"sleep"` is a 422 with
    a `metric` field error.
  - the Catalog build test covers guard 2.
  - `handlers_test.go`: `GET /v1/series?metric=sleep` returns the stacked
    payload — points carrying `states`, a `nights` count — and a non-sleep
    Metric's response carries neither key.

## Why here

Sleep being a Metric is what makes this issue short: there is no sleep endpoint
to write, and the Ledger needs one row-source addition rather than a second
page. The only real work is the two divisors and the two guards, and all four
are about the same thing — a number that is arithmetically fine and
semantically false. A 30-day mean over 21 nights, and a typed sleep row sitting
in a table the sleep read path does not consult, are both silent.

## Comments

**Guard 1 already existed, on both sides.** `validateManualMetric` has refused a
`duration_by_state` Metric since the manual-entry milestone
(`internal/api/measurementhandlers.go:203`), and the dialog already filters the
same rule out of its Metric list (`manual-entry-dialog.tsx:160`). Only the test
case was missing; it is there now.

**`foldFigure` takes the Series, not the summary.** It needed `Nights`, and
taking the whole Series also let the `agg` parameter go: a Series already
carries its own aggregation. Three call sites, one fewer argument each.

**A bug the API test found before it was written.** The Night range was derived
by truncating the window's bounds to their day, which is right for the
day-aligned windows timeaxis resolves and wrong for the now-relative ones the
Ledger builds: the last seven days end at *this morning's date, exclusive*, so
last night — the night a person most wants to see — would have been missing from
the scoreboard until the following day. Both bounds now round up to a day
boundary (`nightRange`), which leaves day-aligned windows identical, keeps the
count right (seven days, seven Nights), and includes last night. Pinned by
`TestSleepNowRelativeWindowKeepsLastNight`.
