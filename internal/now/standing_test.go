package now

import (
	"context"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// follow puts metrics on a Panel of a new Dashboard, which is what makes a Metric
// followed and so a candidate for Unusual.
func follow(t *testing.T, models data.Models, acc int64, metrics ...string) {
	t.Helper()
	ctx := context.Background()
	d := &data.Dashboard{AccountID: acc, Name: "Mine", RangePreset: "last_30d", BaselineRule: "none"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("insert dashboard: %v", err)
	}
	for _, m := range metrics {
		p := &data.Panel{DashboardID: d.ID, AccountID: acc, Width: 1, Metrics: []data.PanelMetric{{Metric: m, ChartType: "line"}}}
		if err := models.Panels.Insert(ctx, p); err != nil {
			t.Fatalf("insert panel: %v", err)
		}
	}
}

func unusual(t *testing.T, e Engine, acc int64) []Card {
	t.Helper()
	out, err := e.Unusual(context.Background(), acc, today22)
	if err != nil {
		t.Fatalf("Unusual: %v", err)
	}
	return out
}

func read(t *testing.T, e Engine, acc int64) Read {
	t.Helper()
	out, err := e.Read(context.Background(), acc, today22)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return out
}

// A followed Metric outside its Usual is raised, the furthest out first; one within
// it is not, a pinned one is left to its own card, and one nobody follows is not
// read at all.
func TestUnusualRanksTheFurthestOutFirst(t *testing.T) {
	e, models, acc := setup(t)
	// 1..28 has a usual of 7.75 to 21.25 (width 13.5).
	seed(t, models, acc, daily("resting_heart_rate", "2026-08-24", append(oneTo(28), 99)...)...)         // far out
	seed(t, models, acc, daily("body_mass", "2026-08-24", append(oneTo(28), 25)...)...)                  // just out
	seed(t, models, acc, daily("walking_heart_rate_average", "2026-08-24", append(oneTo(28), 14)...)...) // within
	seed(t, models, acc, daily("respiratory_rate", "2026-08-24", append(oneTo(28), 99)...)...)           // pinned
	seed(t, models, acc, daily("dietary_copper", "2026-08-24", append(oneTo(28), 99)...)...)             // not followed
	follow(t, models, acc, "resting_heart_rate", "body_mass", "walking_heart_rate_average", "respiratory_rate")
	pin(t, models, acc, "respiratory_rate")

	got := unusual(t, e, acc)
	if len(got) != 2 || got[0].Metric != "resting_heart_rate" || got[1].Metric != "body_mass" {
		t.Fatalf("unusual = %+v", got)
	}
	if got[0].Latest == nil || got[0].Latest.Usual == nil {
		t.Fatalf("an unusual card carries its Usual: %+v", got[0])
	}
}

// A Metric whose last value is more than a week behind the Account's last datum is
// not raised: it stopped, and History is where a stop is read.
func TestUnusualIgnoresAMetricThatStopped(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, daily("resting_heart_rate", "2026-07-01", append(oneTo(28), 99)...)...)
	seed(t, models, acc, steps("Apple Watch", "2026-09-21T08:00:00Z"))
	follow(t, models, acc, "resting_heart_rate")

	if got := unusual(t, e, acc); len(got) != 0 {
		t.Fatalf("unusual = %+v, want none", got)
	}
}

func TestOutsideIsInBandWidths(t *testing.T) {
	u := query.Usual{Low: 10, High: 20}
	for _, c := range []struct{ v, want float64 }{{15, 0}, {10, 0}, {25, 0.5}, {0, 1}} {
		if got := outside(c.v, u); got != c.want {
			t.Errorf("outside(%v) = %v, want %v", c.v, got, c.want)
		}
	}
	if got := outside(12, query.Usual{Low: 10, High: 10}); got != 0.2 {
		t.Errorf("zero-width band: %v", got)
	}
}

// The last Night is the sleep Metric's Latest day, read as the entity, with its age.
func TestLastNightIsTheLatestSleep(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, "Apple Watch", "2026-09-18T23:00:00Z", "2026-09-19T06:00:00Z")
	seedSleep(t, models, acc, "Apple Watch", "2026-09-19T23:00:00Z", "2026-09-20T06:30:00Z")

	n := read(t, e, acc).LastNight
	if n == nil || n.Night != "2026-09-20" || n.AgeDays != 2 || n.Asleep != 450 || len(n.Intervals) != 1 {
		t.Fatalf("last night = %+v", n)
	}
}

func TestNoSleepNoNight(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc, steps("Apple Watch", "2026-09-21T08:00:00Z"))
	if n := read(t, e, acc).LastNight; n != nil {
		t.Fatalf("last night = %+v, want none", n)
	}
}

// The last workout is the Session that began last, aged from the day it began.
func TestLastWorkoutIsTheOneThatBeganLast(t *testing.T) {
	e, models, acc := setup(t)
	for i, at := range []string{"2026-09-15T18:00:00Z", "2026-09-19T07:00:00Z", "2026-09-10T07:00:00Z"} {
		s := &data.Session{
			AccountID: acc, ActivityType: "running", StartAt: at, EndAt: at, Duration: 1800,
			Source: "Apple Watch", ContentKey: "run-" + string(rune('a'+i)),
		}
		if _, err := models.Sessions.InsertSession(context.Background(), s); err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}
	w := read(t, e, acc).LastWorkout
	if w == nil || w.Session.StartAt != "2026-09-19T07:00:00Z" || w.AgeDays != 3 {
		t.Fatalf("last workout = %+v", w)
	}
}

// Every Goal in force today is listed, pinned or not; one that ended is not.
func TestGoalRowsAreTheGoalsInForceToday(t *testing.T) {
	e, models, acc := setup(t)
	ctx := context.Background()
	if _, err := models.Goals.Open(ctx, acc, "steps", data.GoalAtLeast, 1500, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Goals.Open(ctx, acc, "dietary_energy", data.GoalAtMost, 2300, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	seed(t, models, acc, data.Measurement{Metric: "steps", Value: 2000, StartAt: "2026-09-20T08:00:00Z", Source: "W"})

	rows := read(t, e, acc).Goals
	if len(rows) != 2 {
		t.Fatalf("goals = %+v", rows)
	}
	byMetric := map[string]GoalRow{}
	for _, r := range rows {
		byMetric[r.Metric] = r
	}
	st := byMetric["steps"]
	if st.Goal.Value != 1500 || st.Attainment == nil || st.Attainment.Met != 1 || st.Attainment.Measured != 1 {
		t.Fatalf("steps goal = %+v %+v", st, st.Attainment)
	}
	if byMetric["dietary_energy"].Goal.Direction != data.GoalAtMost {
		t.Fatalf("dietary goal = %+v", byMetric["dietary_energy"])
	}
}
