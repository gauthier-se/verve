package now

import (
	"context"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

var today22 = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

func pin(t *testing.T, models data.Models, acc int64, metrics ...string) {
	t.Helper()
	for _, m := range metrics {
		if _, err := models.Pins.Add(context.Background(), acc, m); err != nil {
			t.Fatalf("pin %s: %v", m, err)
		}
	}
}

func cards(t *testing.T, e Engine, acc int64) []Card {
	t.Helper()
	out, err := e.Cards(context.Background(), acc, today22)
	if err != nil {
		t.Fatalf("Cards: %v", err)
	}
	return out
}

func card(t *testing.T, cs []Card, metric string) Card {
	t.Helper()
	for _, c := range cs {
		if c.Metric == metric {
			return c
		}
	}
	t.Fatalf("no card for %s in %+v", metric, cs)
	return Card{}
}

// Nothing is seeded: an Account with no Pin has no card (ADR 0025).
func TestNoPinNoCard(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, steps("Apple Watch", "2026-09-21T08:00:00Z"))
	if got := cards(t, e, acc); len(got) != 0 {
		t.Fatalf("cards without a Pin: %+v", got)
	}
}

// The case this screen exists for: a Metric that stopped 35 days ago keeps its last
// value, with its date and its age, instead of the "—" a 30-day window shows.
func TestCardKeepsAValueFromFiveWeeksAgo(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		steps("Apple Watch", "2026-08-18T08:00:00Z"),
		steps("Apple Watch", "2026-08-18T18:00:00Z"),
	)
	pin(t, models, acc, "steps")

	c := card(t, cards(t, e, acc), "steps")
	if c.Latest == nil || c.Latest.Value != 2000 || c.Latest.Date != "2026-08-18" || c.Latest.AgeDays != 35 {
		t.Fatalf("latest: %+v", c.Latest)
	}
}

// A Pin never measured is a card that says so, not a missing card.
func TestCardOfAMetricNeverMeasured(t *testing.T) {
	e, models, acc := setup(t)
	pin(t, models, acc, "resting_heart_rate")
	c := card(t, cards(t, e, acc), "resting_heart_rate")
	if c.Latest != nil {
		t.Fatalf("latest of a Metric never measured: %+v", c.Latest)
	}
}

// Cards follow Pin order, which is sidebar order.
func TestCardsFollowPinOrder(t *testing.T) {
	e, models, acc := setup(t)
	pin(t, models, acc, "steps", "body_mass", "active_energy")
	got := cards(t, e, acc)
	want := []string{"steps", "body_mass", "active_energy"}
	for i := range want {
		if got[i].Metric != want[i] {
			t.Fatalf("order: %+v", got)
		}
	}
}

// A typed value displaces its day (ADR 0022), so the Latest value on that day is the
// Manual one, as the bar a Panel draws there is.
func TestLatestValueHonoursTheManualOverlay(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "body_mass", Value: 80, OriginalUnit: "kg", StartAt: "2026-09-20T07:00:00Z", Source: "Scale"},
		data.Measurement{Metric: "body_mass", Value: 78.5, OriginalUnit: "kg", StartAt: "2026-09-20T08:00:00Z", Source: "Manual"},
	)
	pin(t, models, acc, "body_mass")

	c := card(t, cards(t, e, acc), "body_mass")
	if c.Latest == nil || c.Latest.Value != 78.5 || c.Latest.AgeDays != 2 {
		t.Fatalf("latest: %+v", c.Latest)
	}
	if c.Goal != nil || c.Attainment != nil {
		t.Fatalf("a latest Metric has no Goal: %+v", c)
	}
}

// The Latest value is the day bucket /v1/series serves for the same date, byte for
// byte: Source election included.
func TestLatestValueEqualsTheSeriesPoint(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		steps("Apple Watch", "2026-09-10T08:00:00Z"),
		steps("iPhone", "2026-09-10T09:00:00Z"),
		steps("iPhone", "2026-09-10T10:00:00Z"),
	)

	latest, err := e.Query.Latest(context.Background(), acc, "steps")
	if err != nil || latest == nil {
		t.Fatalf("Latest: %v %+v", err, latest)
	}
	s, err := e.Query.Series(context.Background(), query.Request{
		AccountID: acc, Metric: "steps", Bucket: timeaxis.Day,
		From: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || len(s.Points) != 1 {
		t.Fatalf("Series: %v %+v", err, s.Points)
	}
	if latest.Value != s.Points[0].Value || latest.Date != s.Points[0].Bucket {
		t.Fatalf("latest %+v, series point %+v", latest, s.Points[0])
	}
}

// Sleep reports its last Night's label, the morning it woke into.
func TestLatestSleepIsTheLastNight(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, "Apple Watch", "2026-09-19T23:00:00Z", "2026-09-20T06:00:00Z")
	pin(t, models, acc, "sleep")

	c := card(t, cards(t, e, acc), "sleep")
	if c.Latest == nil || c.Latest.Date != "2026-09-20" || c.Latest.Value != 420 {
		t.Fatalf("latest sleep: %+v", c.Latest)
	}
}

// The card carries the bound in force today and counts the seven days before it:
// today is never judged, and a gap is neither met nor missed.
func TestCardCountsTheWeekBeforeToday(t *testing.T) {
	e, models, acc := setup(t)
	if _, err := models.Goals.Open(context.Background(), acc, "steps", data.GoalAtLeast, 1500, "2026-09-01"); err != nil {
		t.Fatalf("open goal: %v", err)
	}
	seed(t, models, acc,
		data.Measurement{Metric: "steps", Value: 2000, StartAt: "2026-09-15T08:00:00Z", Source: "W"}, // met
		data.Measurement{Metric: "steps", Value: 1000, StartAt: "2026-09-16T08:00:00Z", Source: "W"}, // missed
		data.Measurement{Metric: "steps", Value: 3000, StartAt: "2026-09-21T08:00:00Z", Source: "W"}, // met
		data.Measurement{Metric: "steps", Value: 9000, StartAt: "2026-09-22T08:00:00Z", Source: "W"}, // today, not judged
		data.Measurement{Metric: "steps", Value: 9000, StartAt: "2026-09-14T08:00:00Z", Source: "W"}, // before the week
	)
	pin(t, models, acc, "steps")

	c := card(t, cards(t, e, acc), "steps")
	if c.Goal == nil || c.Goal.Value != 1500 || c.Goal.Direction != data.GoalAtLeast {
		t.Fatalf("goal: %+v", c.Goal)
	}
	a := c.Attainment
	if a == nil || a.Covered != 7 || a.Measured != 3 || a.Met != 2 {
		t.Fatalf("attainment: %+v", a)
	}
	if c.Latest == nil || c.Latest.Date != "2026-09-22" || c.Latest.AgeDays != 0 {
		t.Fatalf("latest: %+v", c.Latest)
	}
}
