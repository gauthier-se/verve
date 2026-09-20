package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
)

func getDay(t *testing.T, srv *Server, cookie *http.Cookie, date string) (*http.Response, dayView) {
	t.Helper()
	res, body := do(t, srv, "/v1/days/"+date, cookie)
	var view dayView
	if raw, ok := body["day"]; ok {
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatalf("decode day: %v", err)
		}
	}
	return res, view
}

// One date, everything on it, in one call (ADR 0043).
func TestDayEndpointAnswersOneDate(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 6500, OriginalUnit: "count", StartAt: "2024-01-02T08:00:00Z", EndAt: "2024-01-02T08:00:00Z", Source: "Apple Watch", ContentKey: "s1"},
	})
	seedNightStates(t, models, testEmail, []data.State{
		{StateValue: "asleep_core", StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T05:00:00Z", Source: "Apple Watch", ContentKey: "n1"},
	})
	seedSession(t, models, acc.ID, "running", "2024-01-02T18:00:00Z", nil)

	res, view := getDay(t, srv, cookie, "2024-01-02")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if view.Date != "2024-01-02" {
		t.Errorf("date = %q, want the date asked for", view.Date)
	}
	if len(view.Sessions) != 1 || view.Sessions[0].Activity.Slug != "running" {
		t.Errorf("sessions = %+v, want the day's one run rendered as the list renders it", view.Sessions)
	}
	if view.Night == nil || view.Night.Night != "2024-01-02" {
		t.Errorf("night = %+v, want the night that woke into this morning", view.Night)
	}
	// The figures carry the day's steps and the night's minutes, both folded by the
	// engine and neither re-derived here.
	var steps, sleep bool
	for _, m := range view.Metrics {
		switch m.Metric {
		case "steps":
			steps = m.Value != nil && *m.Value == 6500 && m.Source == "Apple Watch"
		case "sleep":
			sleep = m.Value != nil && *m.Value == 360
		}
	}
	if !steps || !sleep {
		t.Errorf("metrics = %+v, want 6500 steps from the Watch and 360 minutes of sleep", view.Metrics)
	}
}

// A date with nothing on it is a 200 with empty collections, never a 404: a date
// exists whether or not anything happened on it. This is what the Day page's
// previous/next navigation walks onto, so it is a designed state and not an edge.
func TestDayEndpointServesAnEmptyDate(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	res, view := getDay(t, srv, cookie, "1998-06-12")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a date nothing happened on", res.StatusCode)
	}
	if view.Metrics == nil || view.Sessions == nil || view.Annotations == nil ||
		view.Manual == nil || view.Exclusions == nil {
		t.Errorf("view = %+v, want empty collections rather than nulls", view)
	}
	if len(view.Metrics) != 0 || view.Night != nil || view.Phase != nil {
		t.Errorf("view = %+v, want nothing on it", view)
	}
}

func TestDayEndpointRefusals(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	tests := map[string]struct {
		target string
		want   int
	}{
		"not a date":      {"/v1/days/last-tuesday", http.StatusUnprocessableEntity},
		"not a real date": {"/v1/days/2024-13-40", http.StatusUnprocessableEntity},
		"unauthenticated": {"/v1/days/2024-01-02", http.StatusUnauthorized},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var c *http.Cookie
			if name != "unauthenticated" {
				c = cookie
			}
			res, _ := do(t, srv, tc.target, c)
			if res.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
}

// A Day is Account-scoped like every other read: another Account's date is empty,
// not forbidden and not somebody else's.
func TestDayEndpointSeesOnlyItsOwnAccount(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), []data.Measurement{{
		AccountID: other.ID, Metric: "steps", Value: 9999, OriginalUnit: "count",
		StartAt: "2024-01-02T08:00:00Z", EndAt: "2024-01-02T08:00:00Z", Source: "Apple Watch", ContentKey: "other-1",
	}}); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	_, view := getDay(t, srv, cookie, "2024-01-02")
	if len(view.Metrics) != 0 {
		t.Errorf("metrics = %+v, want none: that data belongs to another Account", view.Metrics)
	}
}
