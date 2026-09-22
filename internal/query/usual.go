package query

import (
	"context"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// The Usual: where a bucket sits among the owner's own buckets just before it.
//
// "58 bpm" says nothing on its own; "58, when my recent days sit between 46 and 52"
// says everything. The Usual is the p25 to p75 of the Metric's own values over a
// trailing window that ends strictly before the bucket, so a value is never read
// against a set that contains it. It is descriptive and nothing more: no external
// norm and no reference population, the owner compared to themselves (ADR 0046).
//
// It is not Point.Min/Max, which is the spread *inside* one bucket, and not the Band
// (ADR 0032), which is the dense history drawn with its gaps.

// usualReference is how a grain draws a bucket's Usual: how many buckets before it
// make up the reference, how long each is in days, and the fewest recorded ones a
// band is drawn from. Below that minimum the bucket carries none: a quartile of five
// values moves with every value added and describes nothing.
type usualReference struct {
	window, stepDays, min int
}

// usualReferences holds the grains a Usual is drawn at.
//
// A day is read against the 28 days before it: four of every weekday, so pooling
// them overweights none, and short enough that a new habit becomes the usual within
// a month. A week is read against the twelve before it, a season. A month has no
// Usual: it would need most of a year to reach a usable count, and would then
// describe a year rather than a usual.
var usualReferences = map[timeaxis.Bucket]usualReference{
	timeaxis.Day:  {window: 28, stepDays: 1, min: 14},
	timeaxis.Week: {window: 12, stepDays: 7, min: 8},
}

// Usual is a bucket's reference band: descriptive, never a norm.
type Usual struct {
	// Low and High are the p25 and p75 of the values in the window, by linear
	// interpolation between closest ranks (Hyndman and Fan type 7, the default of numpy
	// and of PERCENTILE.INC), stated so two implementations cannot disagree.
	Low  float64 `json:"low"`
	High float64 `json:"high"`
	// N is the number of values behind the band: the evidence, as Count is for a
	// bucket. A band from 14 days reads differently from one from 28.
	N int `json:"n"`
}

// withUsual sets each Point's Usual from the buckets before it: those already in the
// Series, and the ones before From that a second read fetches at the same grain.
//
// Only whole buckets are compared. A window starting mid-week draws its first week
// from part of it, and a week summed over three days set beside weeks summed over
// seven would read as a collapse that never happened. So a bucket the window cuts
// carries no Usual, and the second read runs to the end of the bucket holding From,
// which puts that week back into the reference whole.
func (e Engine) withUsual(ctx context.Context, req Request, out Series) (Series, error) {
	ref, ok := usualReferences[req.Bucket]
	if !ok {
		return out, nil
	}
	step := func(t time.Time, k int) time.Time { return t.AddDate(0, 0, k*ref.stepDays) }
	first, err := time.Parse(time.DateOnly, req.Bucket.Start(req.From))
	if err != nil {
		return Series{}, err
	}

	prior := req
	prior.Usual = false
	prior.From = step(first, -ref.window)
	prior.To = first
	if first.Before(req.From) {
		prior.To = step(first, 1)
	}
	before, err := e.series(ctx, prior)
	if err != nil {
		return Series{}, err
	}

	// The prior read goes second: where both hold a bucket, it is the one the window
	// cut, and the prior read has it whole.
	history := make(map[string]float64, len(before.Points)+len(out.Points))
	for _, p := range out.Points {
		history[p.Bucket] = p.Value
	}
	for _, p := range before.Points {
		history[p.Bucket] = p.Value
	}

	for i := range out.Points {
		at, err := time.Parse(time.DateOnly, out.Points[i].Bucket)
		if err != nil || at.Before(req.From) || step(at, 1).After(req.To) {
			continue // not a bucket the window covers whole
		}
		var vals []float64
		for k := 1; k <= ref.window; k++ {
			if v, ok := history[step(at, -k).Format(time.DateOnly)]; ok {
				vals = append(vals, v)
			}
		}
		if len(vals) < ref.min {
			continue
		}
		sort.Float64s(vals)
		out.Points[i].Usual = &Usual{Low: quantile(vals, 0.25), High: quantile(vals, 0.75), N: len(vals)}
	}
	return out, nil
}

// quantile is the q-th quantile of sorted, non-empty values by linear interpolation
// between closest ranks (type 7).
func quantile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(pos)
	if lo+1 >= len(sorted) {
		return sorted[lo]
	}
	return sorted[lo] + (pos-float64(lo))*(sorted[lo+1]-sorted[lo])
}
