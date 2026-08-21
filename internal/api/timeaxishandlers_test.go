package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// getTimeAxis is a test helper resolving one set of range tokens.
func getTimeAxis(t *testing.T, srv *Server, cookie *http.Cookie, target string) timeAxisView {
	t.Helper()
	res, body := do(t, srv, target, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("timeaxis status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var view timeAxisView
	if err := json.Unmarshal(body["time_axis"], &view); err != nil {
		t.Fatalf("decode time axis: %v", err)
	}
	return view
}

func TestTimeAxisRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, _ := do(t, srv, "/v1/timeaxis?range_preset=1y")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated timeaxis status = %d, want 401", res.StatusCode)
	}
}

// TestTimeAxisResolvesThePresetAndItsGrain locks what the interface prints under a
// Dashboard header: the window's real dates and the bucket its Panels will be drawn
// at, both from the module that owns them.
func TestTimeAxisResolvesThePresetAndItsGrain(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	for _, tc := range []struct {
		preset string
		days   int
		bucket string
	}{
		{"7d", 7, "day"},
		{"30d", 30, "day"},
		{"1y", 365, "week"},
	} {
		view := getTimeAxis(t, srv, cookie, "/v1/timeaxis?range_preset="+tc.preset)
		if view.Bucket != tc.bucket {
			t.Errorf("%s bucket = %q, want %q", tc.preset, view.Bucket, tc.bucket)
		}
		// A leap year makes 1y 366 days, so the span is checked loosely there.
		if diff := view.Range.Days - tc.days; diff < 0 || diff > 1 {
			t.Errorf("%s days = %d, want about %d", tc.preset, view.Range.Days, tc.days)
		}
		if view.Range.To != today.Format(dayLayout) {
			t.Errorf("%s to = %q, want today's half-open bound %q", tc.preset, view.Range.To, today.Format(dayLayout))
		}
		// Last is the day a label prints: the day before the half-open bound, so no
		// caller ever has to subtract one from a bound it did not compute.
		if want := today.AddDate(0, 0, -1).Format(dayLayout); view.Range.Last != want {
			t.Errorf("%s last = %q, want %q", tc.preset, view.Range.Last, want)
		}
		if view.Baseline != nil {
			t.Errorf("%s baseline = %+v, want none without a rule", tc.preset, view.Baseline)
		}
	}
}

// TestTimeAxisResolvesTheComparedWindow locks the sentence naming the compared
// period: a "previous" baseline is the window before this one, of the same length.
func TestTimeAxisResolvesTheComparedWindow(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	view := getTimeAxis(t, srv, cookie, "/v1/timeaxis?range_preset=30d&baseline_rule=previous")
	if view.Baseline == nil {
		t.Fatal("baseline = nil, want the compared window")
	}
	if view.Baseline.Days != view.Range.Days {
		t.Errorf("baseline days = %d, want the range's %d", view.Baseline.Days, view.Range.Days)
	}
	if view.Baseline.To != view.Range.From {
		t.Errorf("baseline ends %q, want it to end where the range starts (%q)", view.Baseline.To, view.Range.From)
	}
}

// TestTimeAxisRefusesComparisonOnAll mirrors ADR 0015 at the endpoint: nothing
// precedes "all", so asking for a baseline over it is a validation error and not an
// empty answer.
func TestTimeAxisRefusesComparisonOnAll(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := do(t, srv, "/v1/timeaxis?range_preset=all&baseline_rule=previous", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("comparison over all status = %d, want 422", res.StatusCode)
	}
}

func TestTimeAxisRejectsUnknownPreset(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := do(t, srv, "/v1/timeaxis?range_preset=6m", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown preset status = %d, want 422", res.StatusCode)
	}
}
