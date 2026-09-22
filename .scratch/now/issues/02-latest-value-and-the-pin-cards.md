Status: done
Blocked by: 01

# 02: query, now, api: the Latest value and the Pin cards on `/v1/now`

## What

- **`Engine.Latest(ctx, accountID, metric)`** in `internal/query`: the Metric's
  value on the last day holding one, with no lookback bound. Find the Metric's
  last datum day (index `(account_id, metric, start_at)`), then read the day
  series over a short window ending there and take the last Point, so Source
  election (ADR 0034) and the Manual overlay (ADR 0022) apply exactly as on a
  Panel. `sleep` answers its last Night. A derived Metric takes the last day
  where its Formula has a value; if none is found within the short window it has
  no Latest value rather than walking back indefinitely.
- **The Pin cards** in `internal/now`:

  ```go
  type Card struct {
      Metric     string            `json:"metric"`
      Unit       string            `json:"unit"`
      Latest     *LatestValue      `json:"latest,omitempty"` // absent: never measured
      Goal       *day.Goal         `json:"goal,omitempty"`   // in force today, as a bound
      Attainment *goal.Attainment  `json:"attainment,omitempty"` // last 7 complete days
  }

  type LatestValue struct {
      Value   float64 `json:"value"`
      Date    string  `json:"date"`
      AgeDays int     `json:"age_days"`
  }
  ```

  Cards follow Pin order. A Pin whose Metric left the Catalog is skipped, as the
  sidebar hides it (ADR 0025). Attainment reuses `internal/goal` over the 7 days
  before today, so today is not judged (ADR 0044); absent when no Goal was in
  force over them.
- **`GET /v1/now`** answers `{"freshness": ..., "cards": [...]}`.

## Tests

- a Metric that stopped 35 days ago has a Latest value, dated, aged 35;
- a day with a Manual entry reports the Manual value as Latest;
- a Latest value agrees with the day bucket `/v1/series` serves for that date;
- `sleep` reports its last Night's label;
- a `latest` Metric (body mass) has a Latest value and never a Goal;
- the Attainment excludes today and matches a Panel's counts over the same 7 days;
- an Account with no Pin answers no cards and its Freshness.

## Comments

Shipped. `Engine.Latest` and `lastDay` in `internal/query/lastday.go`,
`now.Engine.Cards` and `Read` in `internal/now/now.go`, `cards` on `GET /v1/now`,
`NowCard` and `LatestValue` in `types.ts` pinned by the contract test.

What differs from the spec:

- **The Latest value is `query.LatestValue{value, date}`; the age is added by
  `internal/now`** (`now.Latest`), because an age needs a today and the read engine
  has no clock. `04` gives the Ledger the same age the same way.
- **Every last-day lookup is one index seek**, sleep included: the Night label is
  monotonic in `start_at`, so `date(MAX(start_at), '+12 hours')` is the last Night
  and stays on `states_account_kind_start`.
- **A derived Metric's anchor is the earliest of its operands' last days**, then a
  31-day read back from it. `calorie_balance` with a Watch that kept going after the
  food log stopped reports the last day that had both.
- **The card carries `aggregation`** like a `DayMetric`, so the client formats it
  without a Catalog lookup.
