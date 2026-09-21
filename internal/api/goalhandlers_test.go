package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// openGoal posts a Goal and returns the decoded view, failing on anything but a 201.
func openGoal(t *testing.T, srv *Server, cookie *http.Cookie, body map[string]any) goalView {
	t.Helper()
	res, out := send(t, srv, http.MethodPost, "/v1/goals", body, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open %v: status = %d, want 201", body, res.StatusCode)
	}
	var g goalView
	if err := json.Unmarshal(out["goal"], &g); err != nil {
		t.Fatalf("decode goal: %v", err)
	}
	return g
}

// dayAgo is a YYYY-MM-DD n days before today (negative for the future).
func dayAgo(n int) string {
	return time.Now().UTC().AddDate(0, 0, -n).Format(dayLayout)
}

func TestOpenGoalClosesThePreviousOneOnItsMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	first := openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7000, "started_on": dayAgo(30),
	})
	openGoal(t, srv, cookie, map[string]any{
		"metric": "dietary_protein", "direction": "at_least", "value": 150, "started_on": dayAgo(30),
	})
	second := openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7500,
	})
	if second.StartedOn != dayAgo(0) {
		t.Errorf("started_on = %q, want today by default", second.StartedOn)
	}

	res, body := do(t, srv, "/v1/goals?metric=steps", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", res.StatusCode)
	}
	var goals []goalView
	if err := json.Unmarshal(body["goals"], &goals); err != nil {
		t.Fatalf("decode goals: %v", err)
	}
	if len(goals) != 2 || goals[0].ID != second.ID || goals[1].ID != first.ID {
		t.Fatalf("steps history = %+v, want both newest first", goals)
	}
	if goals[1].EndedOn == nil || *goals[1].EndedOn != second.StartedOn {
		t.Errorf("first ended_on = %v, want %s", goals[1].EndedOn, second.StartedOn)
	}
	if goals[0].EndedOn != nil {
		t.Error("the new Goal is not open")
	}

	_, body = do(t, srv, "/v1/goals", cookie)
	if err := json.Unmarshal(body["goals"], &goals); err != nil {
		t.Fatalf("decode all goals: %v", err)
	}
	if len(goals) != 3 {
		t.Errorf("unfiltered list has %d Goals, want 3", len(goals))
	}
}

// TestOpenGoalEligibilityFollowsTheAggregation: the rule is asked, not a list, so a
// derived Metric with a negative bound and both by-state Metrics are accepted and a
// latest one is refused.
func TestOpenGoalEligibilityFollowsTheAggregation(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	for _, body := range []map[string]any{
		{"metric": "calorie_balance", "direction": "at_most", "value": -300},
		{"metric": "sleep", "direction": "at_least", "value": 420},
		{"metric": "training_time", "direction": "at_least", "value": 30},
		{"metric": "resting_heart_rate", "direction": "at_most", "value": 60},
	} {
		openGoal(t, srv, cookie, body)
	}

	for _, body := range []map[string]any{
		{"metric": "body_mass", "direction": "at_most", "value": 75},
		{"metric": "not_a_metric", "direction": "at_least", "value": 1},
		{"direction": "at_least", "value": 1},
		{"metric": "steps", "direction": "between", "value": 1},
		{"metric": "steps", "direction": "at_least"},
		{"metric": "steps", "direction": "at_least", "value": 1, "started_on": "03/01/2026"},
		{"metric": "steps", "direction": "at_least", "value": 1, "started_on": time.Now().UTC().AddDate(0, 0, 2).Format(dayLayout)},
	} {
		res, _ := send(t, srv, http.MethodPost, "/v1/goals", body, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("open %v: status = %d, want 422", body, res.StatusCode)
		}
	}
}

func TestOpenGoalRefusesToRewriteThePast(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7000, "started_on": dayAgo(10),
	})
	res, _ := send(t, srv, http.MethodPost, "/v1/goals", map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7500, "started_on": dayAgo(20),
	}, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("start before the open Goal: status = %d, want 422", res.StatusCode)
	}
}

func TestCloseGoalThenDelete(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	g := openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7000, "started_on": dayAgo(10),
	})
	id := itoa(g.ID)

	// Before the start, or in the future, is refused.
	for _, bad := range []string{dayAgo(10), dayAgo(-2), "soon"} {
		res, _ := send(t, srv, http.MethodPatch, "/v1/goals/"+id, map[string]any{"ended_on": bad}, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("close on %s: status = %d, want 422", bad, res.StatusCode)
		}
	}

	res, body := send(t, srv, http.MethodPatch, "/v1/goals/"+id, map[string]any{}, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("close status = %d, want 200", res.StatusCode)
	}
	var closed goalView
	if err := json.Unmarshal(body["goal"], &closed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if closed.EndedOn == nil || *closed.EndedOn != dayAgo(0) {
		t.Errorf("ended_on = %v, want today by default", closed.EndedOn)
	}

	// Re-closing moves the end date, which rewrites history.
	res, _ = send(t, srv, http.MethodPatch, "/v1/goals/"+id, map[string]any{}, cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("second close status = %d, want 404", res.StatusCode)
	}

	res, _ = send(t, srv, http.MethodDelete, "/v1/goals/"+id, nil, cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", res.StatusCode)
	}
}

func TestGoalEndpointsAreAccountScoped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	otherCookie := login(t, srv, "other@example.com", testPassword)

	g := openGoal(t, srv, otherCookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7000, "started_on": dayAgo(10),
	})
	id := itoa(g.ID)

	if res, _ := send(t, srv, http.MethodPatch, "/v1/goals/"+id, map[string]any{}, cookie); res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account close = %d, want 404", res.StatusCode)
	}
	if res, _ := send(t, srv, http.MethodDelete, "/v1/goals/"+id, nil, cookie); res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account delete = %d, want 404", res.StatusCode)
	}
	_, body := do(t, srv, "/v1/goals", cookie)
	var goals []goalView
	if err := json.Unmarshal(body["goals"], &goals); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(goals) != 0 {
		t.Errorf("owner sees %d Goals of another Account", len(goals))
	}
}

// seriesGoal reads /v1/series and returns the current and Baseline Attainment.
func seriesGoal(t *testing.T, srv *Server, cookie *http.Cookie, qs string) (current, baseline *query.Attainment) {
	t.Helper()
	res, body := do(t, srv, "/v1/series?"+qs, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var cur, base query.Series
	if err := json.Unmarshal(body["series"], &cur); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	if raw, ok := body["baseline"]; ok {
		if err := json.Unmarshal(raw, &base); err != nil {
			t.Fatalf("decode baseline: %v", err)
		}
	}
	return cur.Goal, base.Goal
}

// The counts are the day's whatever the bucket: a weekly Panel over the same dates
// reads the same Attainment as a daily one (ADR 0044).
func TestSeriesAttainmentIgnoresThePanelBucket(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 9000, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
		{Metric: "steps", Value: 3000, OriginalUnit: "count", StartAt: "2024-01-02T08:00:00Z", EndAt: "2024-01-02T08:00:00Z", Source: "Watch", ContentKey: "b"},
		{Metric: "steps", Value: 8000, OriginalUnit: "count", StartAt: "2024-01-09T08:00:00Z", EndAt: "2024-01-09T08:00:00Z", Source: "Watch", ContentKey: "c"},
	})
	openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7500, "started_on": "2023-12-01",
	})

	const window = "metric=steps&range_preset=custom&range_from=2024-01-01&range_to=2024-01-15"
	daily, _ := seriesGoal(t, srv, cookie, window+"&bucket=day")
	weekly, _ := seriesGoal(t, srv, cookie, window+"&bucket=week")
	if daily == nil || weekly == nil {
		t.Fatalf("daily = %+v, weekly = %+v, want both", daily, weekly)
	}
	if daily.Covered != 14 || daily.Measured != 3 || daily.Met != 2 {
		t.Errorf("daily = %+v, want 14 covered, 3 measured, 2 met", daily)
	}
	if weekly.Covered != daily.Covered || weekly.Measured != daily.Measured || weekly.Met != daily.Met {
		t.Errorf("weekly = %+v, want the daily counts %+v", weekly, daily)
	}

	// No Goal on a Metric: no field at all.
	if g, _ := seriesGoal(t, srv, cookie, "metric=heart_rate&range_preset=custom&range_from=2024-01-01&range_to=2024-01-15&bucket=day"); g != nil {
		t.Errorf("heart_rate goal = %+v, want none", g)
	}
}

// A Baseline is judged against the Goal in force over its own window, not today's.
func TestSeriesBaselineIsJudgedAgainstItsOwnGoal(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 7200, OriginalUnit: "count", StartAt: "2023-07-01T08:00:00Z", EndAt: "2023-07-01T08:00:00Z", Source: "Watch", ContentKey: "b1"},
		{Metric: "steps", Value: 7200, OriginalUnit: "count", StartAt: "2024-02-01T08:00:00Z", EndAt: "2024-02-01T08:00:00Z", Source: "Watch", ContentKey: "c1"},
	})
	openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7000, "started_on": "2023-06-01",
	})
	openGoal(t, srv, cookie, map[string]any{
		"metric": "steps", "direction": "at_least", "value": 7500, "started_on": "2024-01-01",
	})

	cur, base := seriesGoal(t, srv, cookie, "metric=steps&range_preset=custom&range_from=2024-02-01&range_to=2024-02-03&bucket=day&baseline_rule=custom&baseline_from=2023-07-01&baseline_to=2023-07-03")
	if cur == nil || cur.Met != 0 || cur.Measured != 1 {
		t.Errorf("current = %+v, want 7 200 missed against 7 500", cur)
	}
	if base == nil || base.Met != 1 || base.Measured != 1 {
		t.Errorf("baseline = %+v, want 7 200 met against the 7 000 in force then", base)
	}
	if base != nil && (len(base.Segments) != 1 || base.Segments[0].Value != 7000) {
		t.Errorf("baseline segments = %+v, want the 7 000 Goal", base.Segments)
	}
}
