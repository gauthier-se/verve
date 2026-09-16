package query

import (
	"math"
	"time"
)

// The trend of a sampled Metric: the signal under the noise, and how fast it moves.
//
// A daily body mass is a noisy sample of a slow quantity. Hydration, glycogen, gut
// content and the hour of weighing move a morning reading by more than a kilogram,
// while the thing its owner is steering by moves by a tenth of one. Drawing only the
// readings shows mostly the part that does not matter, and reading a change off the
// first and last of them is worse still: over the reference Account's 15 July to
// 27 August, the endpoints say −4.85 kg and the fit says −3.96 kg, because 15 July
// happened to be a heavy morning.
//
// Two different estimators, deliberately, because they answer different questions:
//
//   - **The drawn line is an EWMA.** The window is the Account's to choose, and over
//     a year body mass is not a straight line — one least-squares fit across a cut, a
//     holiday and a rebuild draws a single line through three stories and describes
//     none of them. An exponential average follows the curve.
//   - **The quoted rate is a least-squares slope over the whole window.**
//     Differencing an EWMA's ends to get a rate puts back exactly the endpoint
//     sensitivity the fit exists to remove.
//
// So the visible gradient at the right-hand edge of the line will not equal the
// quoted rate on a curved window. That is correct and has to be *said* rather than
// smoothed over: the line answers "where am I", the rate answers "how fast, across
// this whole range".
//
// Both run over the resolved points — after the Source election, the Manual overlay
// (ADR 0022) and Exclusions (ADR 0033) — which is the reason they live here and not
// in a second place that would need its own answer to "which rows count".

const (
	// trendHalfLifeDays is how long a reading keeps half its weight in the average.
	// Ten days is where weight-trend tools have converged since the Hacker's Diet:
	// long enough to absorb a salty dinner and a dehydrated morning, short enough
	// that a real two-week stall reads as a stall instead of being smoothed into the
	// month around it.
	trendHalfLifeDays = 10.0
	// trendRestartGapDays is the silence after which the average starts over rather
	// than carrying a stale prior across it. At three half-lives the prior is down to
	// an eighth, and the first reading after a long gap should describe the body that
	// came back, not the one that left.
	trendRestartGapDays = 3 * trendHalfLifeDays
)

// Trend is the window's fitted direction for a sampled Metric.
//
// PerDay is *not* the visible gradient of the drawn line (see the file comment): the
// line is an EWMA and this is one straight fit over the whole window. A caller that
// shows both must label them apart.
type Trend struct {
	// PerDay and PerWeek are the slope in the Metric's own unit. PerWeek is the figure
	// a person steers by: "−0.65 kg per week" is actionable where "−0.093 kg per day"
	// is arithmetic.
	PerDay  float64 `json:"per_day"`
	PerWeek float64 `json:"per_week"`
	// PctPerWeek is PerWeek against the window *mean*, not the last reading, for the
	// same reason the rate itself is a fit: a percentage of one noisy morning is a
	// noisy percentage.
	PctPerWeek float64 `json:"pct_per_week"`
	// R2 is how much of the movement the straight line accounts for, in [0, 1]. It
	// travels with the slope because they are one claim: −92 g/day at 0.86 describes
	// a window, and the same slope at 0.05 is a line drawn through a cloud. Shown
	// rather than used as a gate — hiding a weak trend would teach a reader that a
	// visible one is always trustworthy.
	R2 float64 `json:"r2"`
	// Days is the number of readings behind the fit: the evidence, as Count is for a
	// bucket.
	Days int `json:"days"`
}

// smoothTrend sets Point.Trend on each point, in place, to the exponentially weighted
// average of the readings up to and including it.
//
// The decay is by **elapsed days**, not by position in the slice: a week without
// weighing must decay the prior by a week, where a positional average would treat the
// next reading as if it were tomorrow's. Buckets carry a date precisely so this can be
// asked.
//
// Points whose bucket is not a day are left untouched, which is also the guard for a
// caller that hands this a non-daily series.
func smoothTrend(points []Point) {
	var (
		avg  float64
		prev time.Time
		have bool
	)
	for i := range points {
		day, err := time.Parse("2006-01-02", points[i].Bucket)
		if err != nil {
			continue
		}
		gap := day.Sub(prev).Hours() / 24
		switch {
		case !have || gap >= trendRestartGapDays:
			avg = points[i].Value // seed, or start over after a long silence
		default:
			w := math.Pow(0.5, gap/trendHalfLifeDays)
			avg = avg*w + points[i].Value*(1-w)
		}
		have, prev = true, day
		v := avg
		points[i].Trend = &v
	}
}

// fitTrend is the least-squares line through the points, with the r² that says how
// much of their movement it explains. Nil when there is nothing a line could describe:
// fewer than two dated readings, or every reading on the same day.
func fitTrend(points []Point) *Trend {
	slope, intercept, ok := leastSquares(points)
	if !ok {
		return nil
	}

	var n, sumY float64
	for _, p := range points {
		if _, err := time.Parse("2006-01-02", p.Bucket); err != nil {
			continue
		}
		n++
		sumY += p.Value
	}
	meanY := sumY / n

	// r² = 1 − SSres/SStot. A window whose readings never vary has no total sum of
	// squares to explain; a flat line through it is a perfect description, so 1 is the
	// honest answer rather than a division by zero.
	var ssRes, ssTot float64
	origin := trendOrigin(points)
	for _, p := range points {
		day, err := time.Parse("2006-01-02", p.Bucket)
		if err != nil {
			continue
		}
		x := day.Sub(origin).Hours() / 24
		r := p.Value - (intercept + slope*x)
		ssRes += r * r
		ssTot += (p.Value - meanY) * (p.Value - meanY)
	}
	r2 := 1.0
	if ssTot > 0 {
		r2 = 1 - ssRes/ssTot
	}
	r2 = math.Max(0, math.Min(1, r2))

	perWeek := slope * 7
	out := Trend{PerDay: slope, PerWeek: perWeek, R2: r2, Days: int(n)}
	if meanY != 0 {
		out.PctPerWeek = perWeek / meanY * 100
	}
	return &out
}

// trendOrigin is the day the fit measures x from: the first dated point. Any origin
// gives the same slope; using the window's own first reading keeps x small, which
// keeps the normal equations well conditioned over a multi-year range.
func trendOrigin(points []Point) time.Time {
	for _, p := range points {
		if day, err := time.Parse("2006-01-02", p.Bucket); err == nil {
			return day
		}
	}
	return time.Time{}
}

// leastSquares fits value against day offset and returns (slope per day, intercept).
// Undated points take no part: they are in no bucket either.
func leastSquares(points []Point) (slope, intercept float64, ok bool) {
	origin := trendOrigin(points)
	var n, sumX, sumY, sumXY, sumXX float64
	for _, p := range points {
		day, err := time.Parse("2006-01-02", p.Bucket)
		if err != nil {
			continue
		}
		x := day.Sub(origin).Hours() / 24
		n++
		sumX += x
		sumY += p.Value
		sumXY += x * p.Value
		sumXX += x * x
	}
	if n < 2 {
		return 0, 0, false
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 || math.IsNaN(denom) {
		return 0, 0, false
	}
	slope = (n*sumXY - sumX*sumY) / denom
	return slope, (sumY - slope*sumX) / n, true
}

// OrdinaryLeastSquares is the slope in units per day of the line through the points,
// measured from origin. Exported for the Estimates engine, which fits body mass the
// same way to back-compute expenditure (ADR 0023) — one fit, so the rate a Panel
// draws and the rate the Plan page reasons from can never be two different methods.
func OrdinaryLeastSquares(points []Point, origin time.Time) (float64, bool) {
	_ = origin // the slope is independent of where x is measured from
	slope, _, ok := leastSquares(points)
	return slope, ok
}
