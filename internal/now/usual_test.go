package now

import (
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// daily is one reading of metric per day from start, the i-th valued vals[i].
func daily(metric, start string, vals ...float64) []data.Measurement {
	d, _ := time.Parse(time.DateOnly, start)
	out := make([]data.Measurement, len(vals))
	for i, v := range vals {
		at := d.AddDate(0, 0, i).Format(time.DateOnly) + "T06:00:00Z"
		out[i] = data.Measurement{Metric: metric, Value: v, StartAt: at, Source: "Apple Watch"}
	}
	return out
}

func oneTo(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i + 1)
	}
	return out
}

// TestCardReadsTheLatestAgainstItsUsual is the sentence the Usual exists for: "58,
// usual 46 to 52". 24 August to 20 September read 1, 2, …, 28 and 21 September, the
// Latest day, reads 99, so the card carries the p25 to p75 of the 28 days before it.
func TestCardReadsTheLatestAgainstItsUsual(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, daily("resting_heart_rate", "2026-08-24", append(oneTo(28), 99)...)...)
	pin(t, models, acc, "resting_heart_rate")

	c := card(t, cards(t, e, acc), "resting_heart_rate")

	if c.Latest == nil || c.Latest.Value != 99 || c.Latest.Date != "2026-09-21" {
		t.Fatalf("latest = %+v, want 99 on 2026-09-21", c.Latest)
	}
	u := c.Latest.Usual
	if u == nil || u.Low != 7.75 || u.High != 21.25 || u.N != 28 {
		t.Fatalf("usual = %+v, want 7.75 to 21.25 from 28", u)
	}
}

// TestTodaysCardHasNoUsual: when the Latest day is today it is still in progress,
// and at 10:00 a step total is unfinished, not below anything.
func TestTodaysCardHasNoUsual(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, daily("steps", "2026-08-25", append(oneTo(28), 3)...)...)
	pin(t, models, acc, "steps")

	c := card(t, cards(t, e, acc), "steps")

	if c.Latest == nil || c.Latest.Date != "2026-09-22" {
		t.Fatalf("latest = %+v, want today", c.Latest)
	}
	if c.Latest.Usual != nil {
		t.Fatalf("usual = %+v on a day in progress, want none", *c.Latest.Usual)
	}
}
