Status: needs-triage
Blocked by: 01

# 02: estimate: search back for a qualifying observed window, and date it

## What

The half that needs the PRD's decision taken first: an observed figure computed over
the most recent 28 days that *meet* coverage, rather than only over the last 28
calendar days.

Do **not** start this until the PRD's question is answered — whether a dated observed
figure outranks a current recorded one. If the answer is no, this issue is `wontfix`
and issue 01 is the whole of the work.

### `internal/estimate/estimate.go`

- **`observedLookbackDays = 90`**, with the reasoning at the constant: three times the
  window, far enough to survive a holiday or a deload, close enough that the body
  being described is still the one asking. Past it the cascade falls through exactly
  as it does today.

- **`observed` walks the window back.** It tries `[now-28d, now)` first, so an Account
  logging today takes the identical path it takes now, then steps the window back
  until it qualifies or passes the lookback. The step is a day and the read is
  already a day-bucketed `query.Series`, so the honest implementation is **one read
  over `[now-90d, now)`** followed by an in-memory scan for the latest qualifying
  28-day slice — not 62 round trips.

- **`Expenditure` carries the window's real bounds**, always, not only when they are
  stale:

  ```go
  WindowFrom string `json:"window_from"` // YYYY-MM-DD, inclusive
  WindowTo   string `json:"window_to"`   // YYYY-MM-DD, exclusive
  ```

  `WindowDays` stays and keeps meaning the span; these say *which* span. A caller that
  shows the figure without the dates is making a claim the data does not support, which
  is the same argument ADR 0023 makes for naming the basis.

### `internal/estimate/target.go`

`ActualRate` has the same defect for the same reason — it reads `[now-28d, now)` and
returns nil below `minMassDays`, so the Plan page's rate slider loses its opening
position at the same moment the expenditure card degrades. It takes the same
backward search over the same window, and the two must agree: a rate fitted over one
window beside an expenditure fitted over another would be two answers about
different months printed as one picture.

### `web/src/components/plan-page.tsx`

When the window does not end today, the card says so. Not a warning banner — a date:

> 3 185 kcal · observed, 31 July – 27 August

and the existing back-computation sentence follows unchanged. A target built on a
dated basis carries the same date wherever the target is shown.

## Why the numbers make the case

On the reference Account, today:

| | Coverage | Slope | Figure |
| --- | --- | --- | --- |
| Current behaviour | 9/28 intake, 9 mass | — | 3 889 kcal (`recorded`) |
| Latest qualifying window, 31 Jul – 27 Aug | 28/28 intake, 24 mass | −119 g/day | **3 185 kcal** (`observed`) |

704 kcal/day, from rows already in the database, at the moment an Account returning
from a break is deciding what to eat.

## Tests

- **`TestObservedPrefersTheMostRecentQualifyingWindow`**: intake dense from day −45 to
  day −20 and absent since; the figure is computed over the dense stretch and
  `WindowTo` is the day after the last logged day, not today.
- **`TestObservedUnchangedWhenTodayQualifies`**: a currently-logging Account gets
  `WindowTo == now`, proving the common path did not move.
- **`TestObservedGivesUpPastTheLookback`**: a qualifying window 120 days back is not
  used; the cascade reaches `recorded` and issue 01's shortfall explains it.
- **`TestActualRateSharesTheObservedWindow`**: the rate's window bounds equal the
  expenditure's, so the page cannot print two windows as one.
