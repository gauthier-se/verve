package query

import (
	"context"
	"math"
	"sort"
	"time"
)

// Co-variation is the read that answers "does this move with that" (CONTEXT.md):
// two Metrics' buckets ranked against each other over one window. It reports the
// strength and the direction of a relationship and nothing else — never a cause,
// never a valence, because Verve cannot know which direction is good for a Metric.
//
// The measure is Spearman's ρ, a correlation of *ranks* rather than of values. That
// choice is not statistical taste: health series are neither normal nor free of
// outliers — one holiday triples a step count, one bad scale reading moves a body
// mass by three kilos — and a Pearson coefficient would let a single bucket write
// the answer. Ranks bound every bucket's influence to one position, and they make a
// relationship that is monotone but not linear (sleep and resting heart rate) still
// legible.

// minSharedFraction is the share of the window's buckets a pair must have in common
// before it is ranked, and minSharedBuckets the floor under it. A coefficient over
// six shared weeks is noise wearing two decimals; refusing to print it is the whole
// point of the rule. The resolved threshold travels with the answer, so the footnote
// under the ranking names the real number rather than a hard-coded one.
const (
	minSharedFraction = 0.6
	minSharedBuckets  = 8
)

// CoVaryRequest is one cross-metric read: the Metrics to pair, the window and
// bucket they are all resolved on, and the Lag in buckets applied to the second
// member of each pair.
type CoVaryRequest struct {
	AccountID int64
	Metrics   []string
	From      time.Time
	To        time.Time
	Bucket    Bucket
	// Lag shifts the second Metric of a pair forward by that many buckets, so a
	// pair reads "A now against B one bucket later". A non-zero Lag makes the
	// matrix directional: (sleep → resting heart rate) is not (resting heart rate
	// → sleep), which is exactly the question a lag is asked for.
	Lag int
}

// Pair is one ordered pair's co-variation: A against B shifted by the request's
// Lag, over the buckets they share.
type Pair struct {
	A string `json:"a"`
	B string `json:"b"`
	// Rho is Spearman's rank correlation in [-1, 1]. Positive is "moves together",
	// negative "moves opposite"; neither is good news.
	Rho float64 `json:"rho"`
	// Shared is how many buckets carried both Metrics — the evidence behind Rho,
	// always shown beside it.
	Shared int `json:"shared"`
	// Ranked is false when Shared fell under the threshold: the pair is computed and
	// shown greyed rather than dropped, because "not enough overlap" is an answer
	// about the data and hiding it would read as "no relationship".
	Ranked bool `json:"ranked"`
}

// Scatter is one pair drawn bucket by bucket: the shared points and a
// least-squares line through them. The line is fitted server-side for the same
// reason the buckets are aggregated there — the client draws what it is given and
// computes no statistics of its own (ADR 0012).
type Scatter struct {
	A      string       `json:"a"`
	B      string       `json:"b"`
	UnitA  string       `json:"unit_a"`
	UnitB  string       `json:"unit_b"`
	Points []ScatterDot `json:"points"`
	Fit    *ScatterFit  `json:"fit,omitempty"`
	Rho    float64      `json:"rho"`
	Shared int          `json:"shared"`
}

// ScatterDot is one shared bucket: the two Metrics' values, and the bucket they
// both fell in so a hover can name it.
type ScatterDot struct {
	Bucket string  `json:"bucket"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// ScatterFit is the fitted line as its two endpoints, in the scatter's own units.
type ScatterFit struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// CoVariation is the whole cross-metric answer for one window: every ordered pair,
// the units each Metric was read in, the threshold that decided what could be
// ranked, and the strongest pair drawn as a scatter.
type CoVariation struct {
	Metrics   []string          `json:"metrics"`
	Units     map[string]string `json:"units"`
	Bucket    Bucket            `json:"bucket"`
	Lag       int               `json:"lag"`
	Pairs     []Pair            `json:"pairs"`
	MinShared int               `json:"min_shared"`
	Strongest *Scatter          `json:"strongest,omitempty"`
}

// CoVary reads every requested Metric over the window once, then pairs them.
//
// The Series each Metric is read as is the same one a Panel draws — one query per
// Metric, the Metric's own rule, the winning Source, the shared bucket grid — so a
// coefficient on this page is computed from exactly the numbers the curve on the
// Dashboard shows. A Metric with no data in the window drops out entirely rather
// than pairing as a column of zeroes.
func (e Engine) CoVary(ctx context.Context, req CoVaryRequest) (CoVariation, error) {
	out := CoVariation{
		Metrics: []string{},
		Units:   map[string]string{},
		Bucket:  req.Bucket,
		Lag:     req.Lag,
		Pairs:   []Pair{},
	}

	// values[metric] is the Metric's buckets keyed by bucket-start date, which is
	// what makes the lag a plain lookup on a neighbouring key rather than an index
	// into two series whose gaps do not line up.
	values := map[string]map[string]float64{}
	for _, slug := range req.Metrics {
		series, err := e.Series(ctx, Request{
			AccountID: req.AccountID, Metric: slug,
			From: req.From, To: req.To, Bucket: req.Bucket,
		})
		if err != nil {
			return CoVariation{}, err
		}
		if len(series.Points) == 0 {
			continue
		}
		byBucket := make(map[string]float64, len(series.Points))
		for _, p := range series.Points {
			byBucket[p.Bucket] = p.Value
		}
		values[slug] = byBucket
		out.Metrics = append(out.Metrics, slug)
		out.Units[slug] = series.Unit
	}

	out.MinShared = thresholdShared(req.Bucket, req.From, req.To)

	var best *Pair
	for _, a := range out.Metrics {
		for _, b := range out.Metrics {
			if a == b {
				continue // the diagonal is a Metric against itself: not a question
			}
			xs, ys, _ := alignPair(values[a], values[b], req.Bucket, req.Lag)
			if len(xs) < 3 {
				out.Pairs = append(out.Pairs, Pair{A: a, B: b, Shared: len(xs)})
				continue
			}
			pair := Pair{A: a, B: b, Rho: spearman(xs, ys), Shared: len(xs)}
			pair.Ranked = pair.Shared >= out.MinShared
			out.Pairs = append(out.Pairs, pair)
			if pair.Ranked && (best == nil || math.Abs(pair.Rho) > math.Abs(best.Rho)) {
				p := pair
				best = &p
			}
		}
	}

	// Pairs come back strongest first, so the ranking is a plain read of the slice
	// and the client sorts nothing. Unranked pairs sink to the bottom whatever their
	// coefficient: an unranked ρ is not a weaker answer, it is not an answer.
	sort.SliceStable(out.Pairs, func(i, j int) bool {
		if out.Pairs[i].Ranked != out.Pairs[j].Ranked {
			return out.Pairs[i].Ranked
		}
		return math.Abs(out.Pairs[i].Rho) > math.Abs(out.Pairs[j].Rho)
	})

	if best != nil {
		out.Strongest = e.scatter(*best, values, out.Units, req)
	}
	return out, nil
}

// scatter draws the given pair bucket by bucket, with a least-squares line through
// the points when there are enough of them to fit one.
func (e Engine) scatter(p Pair, values map[string]map[string]float64, units map[string]string, req CoVaryRequest) *Scatter {
	xs, ys, buckets := alignPair(values[p.A], values[p.B], req.Bucket, req.Lag)
	if len(xs) == 0 {
		return nil
	}
	out := &Scatter{
		A: p.A, B: p.B, UnitA: units[p.A], UnitB: units[p.B],
		Points: make([]ScatterDot, len(xs)), Rho: p.Rho, Shared: p.Shared,
	}
	for i := range xs {
		out.Points[i] = ScatterDot{Bucket: buckets[i], X: xs[i], Y: ys[i]}
	}
	out.Fit = fitLine(xs, ys)
	return out
}

// alignPair walks a's buckets in order and takes b's value from the bucket `lag`
// positions later, returning the two parallel value slices and the a-side bucket
// each pair came from.
//
// The later bucket is found by advancing a's bucket start on the grid, not by
// adding seven days to a date: the boundaries belong to the Bucket, and a lag that
// computed its own would land between buckets in the week a month changes length.
func alignPair(a, b map[string]float64, bucket Bucket, lag int) (xs, ys []float64, buckets []string) {
	xs, ys, buckets = []float64{}, []float64{}, []string{}
	for _, key := range sortedKeys(a) {
		target := key
		if lag != 0 {
			start, err := time.Parse(dayLayout, key)
			if err != nil {
				continue
			}
			for i := 0; i < lag; i++ {
				start = bucket.next(start)
			}
			target = start.Format(dayLayout)
		}
		y, ok := b[target]
		if !ok {
			continue // a bucket only one Metric has is not shared evidence
		}
		xs = append(xs, a[key])
		ys = append(ys, y)
		buckets = append(buckets, key)
	}
	return xs, ys, buckets
}

// thresholdShared is how many shared buckets a pair needs before it may be ranked:
// a share of the window's own bucket count, never below the floor. It scales with
// the range so a three-month window is not silently unrankable, and it is returned
// to the client so the rule can be stated in the interface rather than implied.
func thresholdShared(bucket Bucket, from, to time.Time) int {
	total := len(bucket.Starts(from, to))
	want := int(math.Ceil(float64(total) * minSharedFraction))
	if want < minSharedBuckets {
		want = minSharedBuckets
	}
	if want > total {
		want = total
	}
	return want
}

// spearman is the rank correlation of two equal-length samples: Pearson's r applied
// to the ranks, with ties sharing their average rank. Zero variance on either side
// (every bucket identical, or every value tied) yields 0 — no relationship is
// expressible, which is not the same as a relationship of zero, and both print the
// same way here: nothing to see.
func spearman(xs, ys []float64) float64 {
	if len(xs) != len(ys) || len(xs) < 2 {
		return 0
	}
	return pearson(ranks(xs), ranks(ys))
}

// ranks returns each value's rank within the sample, 1-based, tied values sharing
// the average of the positions they span.
func ranks(vs []float64) []float64 {
	idx := make([]int, len(vs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return vs[idx[i]] < vs[idx[j]] })

	out := make([]float64, len(vs))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && vs[idx[j+1]] == vs[idx[i]] {
			j++
		}
		// Positions i..j are tied: they all take the average rank of that span.
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			out[idx[k]] = avg
		}
		i = j + 1
	}
	return out
}

// pearson is the linear correlation of two equal-length samples, 0 when either
// side has no variance.
func pearson(xs, ys []float64) float64 {
	n := float64(len(xs))
	var sx, sy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
	}
	mx, my := sx/n, sy/n

	var num, dx, dy float64
	for i := range xs {
		a, b := xs[i]-mx, ys[i]-my
		num += a * b
		dx += a * a
		dy += b * b
	}
	if dx == 0 || dy == 0 {
		return 0
	}
	return num / math.Sqrt(dx*dy)
}

// fitLine is the least-squares line through the points, returned as the two
// endpoints spanning the sample's x range. Nil when x has no spread: a vertical
// line through one x value is not a trend, and drawing one would invent a slope.
func fitLine(xs, ys []float64) *ScatterFit {
	n := float64(len(xs))
	if n < 2 {
		return nil
	}
	var sx, sy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
	}
	mx, my := sx/n, sy/n

	var num, den float64
	minX, maxX := xs[0], xs[0]
	for i := range xs {
		a := xs[i] - mx
		num += a * (ys[i] - my)
		den += a * a
		minX = math.Min(minX, xs[i])
		maxX = math.Max(maxX, xs[i])
	}
	if den == 0 {
		return nil
	}
	slope := num / den
	intercept := my - slope*mx
	return &ScatterFit{
		X1: minX, Y1: slope*minX + intercept,
		X2: maxX, Y2: slope*maxX + intercept,
	}
}
