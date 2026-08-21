package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
)

// getCoVary is a test helper reading the cross-metric page for the session.
func getCoVary(t *testing.T, srv *Server, cookie *http.Cookie, target string) coVaryView {
	t.Helper()
	res, body := do(t, srv, target, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("covary status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var view coVaryView
	if err := json.Unmarshal(body["covary"], &view); err != nil {
		t.Fatalf("decode covary: %v", err)
	}
	return view
}

// seedDailyPair writes a rising steps series and a falling resting-heart-rate one
// over the given number of days ending today, so the pair correlates perfectly.
func seedDailyPair(t *testing.T, models data.Models, email string, days int) {
	t.Helper()
	ms := []data.Measurement{}
	for i := 0; i < days; i++ {
		day := daysAgo(i)
		ms = append(ms,
			data.Measurement{Metric: "steps", Value: float64(1000 + i*100), OriginalUnit: "count",
				StartAt: day, EndAt: day, Source: "Watch", ContentKey: fmt.Sprintf("st-%d", i)},
			data.Measurement{Metric: "resting_heart_rate", Value: float64(70 - i), OriginalUnit: "bpm",
				StartAt: day, EndAt: day, Source: "Watch", ContentKey: fmt.Sprintf("hr-%d", i)},
		)
	}
	seedSteps(t, models, email, ms)
}

func TestCoVaryRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, _ := do(t, srv, "/v1/covary?range_preset=1y")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated covary status = %d, want 401", res.StatusCode)
	}
}

// TestCoVaryReadsThePins locks ADR 0025's consequence for this page: the Pins are
// its input, so an Account that has pinned nothing gets an empty matrix rather than
// a guess at which Metrics it cares about.
func TestCoVaryReadsThePins(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDailyPair(t, models, testEmail, 40)

	empty := getCoVary(t, srv, cookie, "/v1/covary?range_preset=30d")
	if len(empty.Metrics) != 0 || len(empty.Pairs) != 0 {
		t.Errorf("unpinned account got %+v, want an empty matrix", empty)
	}

	pin(t, srv, cookie, "steps")
	pin(t, srv, cookie, "resting_heart_rate")

	view := getCoVary(t, srv, cookie, "/v1/covary?range_preset=30d")
	if len(view.Metrics) != 2 {
		t.Fatalf("metrics = %v, want the two pinned ones", view.Metrics)
	}
	if len(view.Pairs) != 2 {
		t.Fatalf("pairs = %+v, want both directions", view.Pairs)
	}
	if view.Bucket != "day" {
		t.Errorf("bucket = %q, want day for a 30-day window", view.Bucket)
	}
	if view.Range.Days != 30 {
		t.Errorf("range days = %d, want 30", view.Range.Days)
	}
	for _, p := range view.Pairs {
		if !p.Ranked {
			t.Errorf("pair %s × %s not ranked on %d shared buckets (threshold %d)", p.A, p.B, p.Shared, view.MinShared)
		}
		if p.Rho > -0.99 {
			t.Errorf("pair %s × %s rho = %v, want ≈ -1", p.A, p.B, p.Rho)
		}
	}
	if view.Strongest == nil || view.Strongest.Fit == nil {
		t.Errorf("strongest = %+v, want the pair drawn with its fitted line", view.Strongest)
	}
}

// TestCoVaryLagPicksItsOwnGrain locks that a lag preset carries the grain the
// question is asked at: "+1 day" is a day-grain read even when the window's own
// bucket would be coarser.
func TestCoVaryLagPicksItsOwnGrain(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDailyPair(t, models, testEmail, 60)
	pin(t, srv, cookie, "steps")
	pin(t, srv, cookie, "resting_heart_rate")

	// A 3-month window buckets weekly on its own.
	same := getCoVary(t, srv, cookie, "/v1/covary?range_preset=3m")
	if same.Bucket != "week" || same.LagShift != 0 {
		t.Errorf("same lag = %q/%d, want week/0", same.Bucket, same.LagShift)
	}

	next := getCoVary(t, srv, cookie, "/v1/covary?range_preset=3m&lag=next_day")
	if next.Bucket != "day" || next.LagShift != 1 {
		t.Errorf("next_day lag = %q/%d, want day/1", next.Bucket, next.LagShift)
	}
}

func TestCoVaryRejectsUnknownLag(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := do(t, srv, "/v1/covary?range_preset=1y&lag=next_fortnight", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown lag status = %d, want 422", res.StatusCode)
	}
}

// TestCoVaryNamesWhatItSkipped locks that a pinned Metric absent from the matrix is
// accounted for: a Pin the Account cannot find on this page is a bug until the page
// says why.
func TestCoVaryNamesWhatItSkipped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDailyPair(t, models, testEmail, 20)
	pin(t, srv, cookie, "steps")
	pin(t, srv, cookie, "body_mass") // pinned, never recorded

	view := getCoVary(t, srv, cookie, "/v1/covary?range_preset=30d")
	if len(view.Skipped) != 1 || view.Skipped[0].Metric != "body_mass" {
		t.Fatalf("skipped = %+v, want body_mass named", view.Skipped)
	}
	if view.Skipped[0].Reason == "" {
		t.Error("skipped metric carries no reason")
	}
}
