Status: done

# 01: query, catalog: a Latest Series carries a smoothed line and a fitted rate

## What

### `internal/query` — the wire shape

`Point` gains one optional field, beside `min`/`max`/`count`:

```go
// Trend is the smoothed value at this bucket: an exponentially weighted moving
// average over the readings, in the Metric's own unit. Nil on a bucket with no
// reading, so a smoothed line breaks across the same gaps the series does
// (ADR 0032) rather than asserting a path nobody weighed.
Trend *float64 `json:"trend,omitempty"`
```

`Series` gains the fitted rate, as its own struct so the fields cannot drift apart:

```go
// Trend is the window's fitted direction: the least-squares slope over the
// resolved readings, with the r² that says how much of the movement it explains.
//
// The slope is *not* the visible gradient of the drawn line. The line is an EWMA,
// which follows a curve; this is one straight fit over the whole window. They answer
// different questions ("where am I" and "how fast, over this window") and a caller
// must label them apart.
type Trend struct {
	PerDay     float64 `json:"per_day"`      // Metric units per day
	PerWeek    float64 `json:"per_week"`     // PerDay * 7, the figure a person steers by
	PctPerWeek float64 `json:"pct_per_week"` // PerWeek / window mean * 100
	R2         float64 `json:"r2"`           // [0, 1]; low means the line describes a cloud
	Days       int     `json:"days"`         // readings behind the fit, the honest denominator
}
```

`PctPerWeek` uses the window mean rather than the last reading, for the same reason
`ActualRate` does: a percentage of one noisy morning is a noisy percentage.

### Where it is computed

In the query engine, after `resolveSource`, on the resolved points — which is what
makes the Manual overlay (ADR 0022) and Exclusions (ADR 0033) apply without a line
of extra code. Computing it anywhere else would be a second answer to "which rows
count".

Gate it on `Aggregation == catalog.Latest` and on the bucket being `Day`. A weekly
or monthly bucket has already smoothed the readings by folding them, and smoothing
a smoothing is a claim about data that is no longer there.

### The smoother

Standard EWMA with a half-life expressed in days rather than a bare α, because a
half-life survives a gap and an α does not:

```go
// trendHalfLifeDays is how long a reading keeps half its weight. Ten days is the
// span the Hacker's Diet and every weight-trend tool since has converged on: long
// enough to absorb a salty dinner and a dehydrated morning, short enough that a real
// two-week stall is visible as a stall rather than smoothed into the month around it.
const trendHalfLifeDays = 10
```

Weight each reading by elapsed days since the previous one, not by position in the
slice, so a week without weighing decays the prior properly instead of treating the
next reading as if it were tomorrow's. Seed on the first reading. A gap longer than,
say, three half-lives restarts the average rather than carrying a month-old prior
across it — the line breaks there anyway, and a restart keeps the first point after
a gap from being a memory of before it.

### `ordinaryLeastSquares` moves

It currently lives in `internal/estimate/estimate.go` and would now have two callers
in two packages. Move it to wherever `query` and `estimate` can both reach it
without `estimate` importing `query`'s internals or vice versa — `internal/units`
is arithmetic-shaped and already shared, or a small `internal/fit`. Whichever it is,
`estimate`'s behaviour must not change: `TestActualRateIsPercentOfBodyMassPerWeek`
and the observed-basis tests are the regression net.

## Tests

- **`TestTrendSmoothsAKnownSawtooth`**: readings alternating ±1.5 kg around a flat
  90 kg produce a trend within a few hundred grams of 90 throughout, while `value`
  keeps swinging — the property the feature exists for.
- **`TestTrendSlopeMatchesTheReferenceWindow`**: the reference Account's 15 July –
  27 August body mass fits to −92 g/day ± 2 g and r² 0.86 ± 0.01, which is the number
  in the PRD and the one a reviewer can check by hand.
- **`TestTrendIsNilOnAGapBucket`**: a day with no reading carries no trend, so the
  client's line breaks rather than interpolating (ADR 0032).
- **`TestTrendWeightsByElapsedDays`**: two series with identical readings but
  different spacing produce different trends, proving the decay is temporal and not
  positional.
- **`TestTrendAbsentForNonLatestAggregation`**: a `sum` Metric carries none, so the
  scope holds at the seam rather than in the UI.
- **`TestTrendAbsentAboveDayBucket`**: a monthly request carries none.
- **`TestTrendFollowsTheManualOverlay`**: a Manual correction on one day moves the
  trend, which is the proof it is fitted to resolved rows and not to raw ones.

## Comments

**Implemented.** `internal/query/trend.go` holds the EWMA and the fit;
`Point.Trend` and `Series.Trend` are on the wire and pinned by
`TestWireContractMatchesTypeScript` (`Trend` added to the contract registry).

Two departures from the spec above, both deliberate:

- **`ordinaryLeastSquares` did not move to a new package.** `estimate` already
  imports `query`, and the fit operates on `[]query.Point`, so the natural home was
  `query` itself. It is exported as `query.OrdinaryLeastSquares` and `estimate` keeps
  its private name as a one-line forwarder, so nothing in that package's call sites
  changed. `TestOrdinaryLeastSquaresIsOriginIndependent` pins the property that lets
  one fit serve both callers: `estimate` measures x from its window start, `query`
  from the first reading.
- **The smoothing test asserts two bounds, not one.** The reference window has two
  three-day gaps, and across a gap the decay correctly gives the returning reading
  more weight (19% at three days against 6.7% at one), so the step there is larger by
  design. A single bound would either pass a broken smoother or test the gaps instead
  of the smoothing. Consecutive days are held to 0.2 kg, gap steps to 0.6.

The reference case passes on the real numbers: −0.0921 kg/day, −0.645 kg/week,
r² 0.863, 40 readings.
