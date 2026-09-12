package query

import (
	"context"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

func readNight(t *testing.T, e Engine, acc int64, label string) (NightDetail, bool) {
	t.Helper()
	night, ok, err := e.Night(context.Background(), acc, label)
	if err != nil {
		t.Fatalf("Night: %v", err)
	}
	return night, ok
}

// A staged night: the intervals in order, onset and wake at the edges of the
// sleep, and the awake row counted because it falls between them.
func TestNightDescribesAStagedNight(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T02:00:00Z", "Watch"},
		{"awake", "2024-01-02T02:00:00Z", "2024-01-02T02:30:00Z", "Watch"},
		{"asleep_deep", "2024-01-02T02:30:00Z", "2024-01-02T05:00:00Z", "Watch"},
	})

	night, ok := readNight(t, e, acc, "2024-01-02")
	if !ok {
		t.Fatal("the night was not found")
	}
	if len(night.Intervals) != 3 || night.Intervals[0].State != "asleep_core" {
		t.Fatalf("intervals = %+v, want the three in start order", night.Intervals)
	}
	if night.Asleep != 330 {
		t.Errorf("asleep = %v, want 330 min (awake excluded)", night.Asleep)
	}
	if night.Onset == nil || *night.Onset != "2024-01-01T23:00:00Z" {
		t.Errorf("onset = %v, want the first asleep start", night.Onset)
	}
	if night.Wake == nil || *night.Wake != "2024-01-02T05:00:00Z" {
		t.Errorf("wake = %v, want the last asleep end", night.Wake)
	}
	if night.Awake == nil || *night.Awake != 30 {
		t.Errorf("awake = %v, want the 30 min interruption", night.Awake)
	}
	// 330 asleep over a 360-minute span from falling asleep to waking.
	if night.Efficiency == nil || *night.Efficiency < 91.6 || *night.Efficiency > 91.7 {
		t.Errorf("efficiency = %v, want ~91.67%%", night.Efficiency)
	}
	if night.EfficiencyBasis != "onset_to_wake" {
		t.Errorf("basis = %q, want the span it was actually computed over", night.EfficiencyBasis)
	}
	if night.Source != "Watch" {
		t.Errorf("source = %q, want the elected one", night.Source)
	}
}

// The page and the bar must agree about the same night, which is why both run
// through one resolution rather than two.
func TestNightAgreesWithTheMetric(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T02:00:00Z", "Watch"},
		{"in_bed", "2024-01-01T22:30:00Z", "2024-01-02T06:00:00Z", "iPhone"},
		{"asleep_rem", "2024-01-02T02:00:00Z", "2024-01-02T04:00:00Z", "Watch"},
	})

	series := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z", timeaxis.Day)
	night, ok := readNight(t, e, acc, "2024-01-02")
	if !ok {
		t.Fatal("the night was not found")
	}
	if len(series.Points) != 1 {
		t.Fatalf("points = %+v, want one night", series.Points)
	}
	if series.Points[0].Value != night.Asleep {
		t.Errorf("the bar says %v and the page says %v for the same night",
			series.Points[0].Value, night.Asleep)
	}
	// The loser Source's in-bed row is absent from both, by the same rule.
	for _, in := range night.Intervals {
		if in.State == "in_bed" {
			t.Errorf("an in-bed interval from the losing Source reached the page: %+v", in)
		}
	}
}

// An iPhone-only night has minutes and no shape: it is a duration in bed, and
// naming the moment it began as an onset would invent a fact.
func TestNightWithoutStagesHasNoOnset(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"in_bed", "2024-01-01T23:00:00Z", "2024-01-02T06:00:00Z", "iPhone"},
	})

	night, ok := readNight(t, e, acc, "2024-01-02")
	if !ok {
		t.Fatal("the night was not found")
	}
	if night.Asleep != 420 {
		t.Errorf("asleep = %v, want the in-bed fallback of 420 min", night.Asleep)
	}
	if night.Onset != nil || night.Wake != nil || night.Efficiency != nil || night.Awake != nil {
		t.Errorf("night = %+v, want no onset, wake, awake or efficiency", night)
	}
	if night.EfficiencyBasis != "" {
		t.Errorf("basis = %q, want none when there is no efficiency", night.EfficiencyBasis)
	}
}

// Awake before falling asleep or after waking is being up, not a fragmented
// night.
func TestNightCountsOnlyTheInterruptions(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"awake", "2024-01-01T22:00:00Z", "2024-01-01T23:00:00Z", "Watch"},
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T03:00:00Z", "Watch"},
		{"awake", "2024-01-02T03:00:00Z", "2024-01-02T03:15:00Z", "Watch"},
		{"asleep_core", "2024-01-02T03:15:00Z", "2024-01-02T06:00:00Z", "Watch"},
		{"awake", "2024-01-02T06:00:00Z", "2024-01-02T06:45:00Z", "Watch"},
	})

	night, ok := readNight(t, e, acc, "2024-01-02")
	if !ok {
		t.Fatal("the night was not found")
	}
	if night.Awake == nil || *night.Awake != 15 {
		t.Errorf("awake = %v, want only the 15 min between onset and waking", night.Awake)
	}
}

// A night nothing recorded is not an empty night.
func TestNightNotRecorded(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T05:00:00Z", "Watch"},
	})

	if _, ok := readNight(t, e, acc, "2024-02-14"); ok {
		t.Error("a night with no rows was reported as found")
	}
}

func TestNightIsScopedToOneAccount(t *testing.T) {
	e, models, acc := setup(t)
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	seedSleep(t, models, other.ID, []interval{
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T05:00:00Z", "Watch"},
	})

	if _, ok := readNight(t, e, acc, "2024-01-02"); ok {
		t.Error("another Account's night was readable")
	}
}
