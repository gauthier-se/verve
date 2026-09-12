package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

func seedNightStates(t *testing.T, models data.Models, email string, rows []data.State) {
	t.Helper()
	acc, err := models.Accounts.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	for i := range rows {
		rows[i].AccountID = acc.ID
		rows[i].Kind = "sleep"
	}
	if _, err := models.States.InsertStateBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed states: %v", err)
	}
}

// A Night is addressed by the morning it woke on, and carries the shape a bucket
// destroys (ADR 0041).
func TestNightEndpointAnswersOneNight(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedNightStates(t, models, testEmail, []data.State{
		{StateValue: "asleep_core", StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T02:00:00Z", Source: "Apple Watch", ContentKey: "n1"},
		{StateValue: "awake", StartAt: "2024-01-02T02:00:00Z", EndAt: "2024-01-02T02:30:00Z", Source: "Apple Watch", ContentKey: "n2"},
		{StateValue: "asleep_deep", StartAt: "2024-01-02T02:30:00Z", EndAt: "2024-01-02T05:00:00Z", Source: "Apple Watch", ContentKey: "n3"},
	})

	res, body := do(t, srv, "/v1/nights/2024-01-02", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var night query.NightDetail
	if err := json.Unmarshal(body["night"], &night); err != nil {
		t.Fatalf("decode night: %v", err)
	}
	if len(night.Intervals) != 3 {
		t.Fatalf("intervals = %+v, want three", night.Intervals)
	}
	if night.Asleep != 330 || night.Awake == nil || *night.Awake != 30 {
		t.Errorf("night = %+v, want 330 asleep and 30 awake", night)
	}
	if night.Onset == nil || night.Wake == nil || night.EfficiencyBasis != "onset_to_wake" {
		t.Errorf("night = %+v, want the span and the basis it was computed over", night)
	}
}

func TestNightEndpointRefusals(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedNightStates(t, models, testEmail, []data.State{
		{StateValue: "asleep_core", StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T05:00:00Z", Source: "Apple Watch", ContentKey: "n1"},
	})

	tests := map[string]struct {
		target string
		want   int
	}{
		"a night nothing recorded": {"/v1/nights/2024-02-14", http.StatusNotFound},
		"not a date":               {"/v1/nights/last-tuesday", http.StatusUnprocessableEntity},
		"a day, not a night label": {"/v1/nights/2024-13-40", http.StatusUnprocessableEntity},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			res, _ := do(t, srv, tc.target, cookie)
			if res.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
}

func TestNightEndpointRequiresAuth(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res, _ := do(t, srv, "/v1/nights/2024-01-02")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}

// Another Account's night is a 404, never a body that confirms it exists
// (ADR 0007).
func TestNightEndpointIsScopedToTheAccount(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	seedNightStates(t, models, "other@example.com", []data.State{
		{StateValue: "asleep_core", StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T05:00:00Z", Source: "Apple Watch", ContentKey: "n1"},
	})

	res, _ := do(t, srv, "/v1/nights/2024-01-02", cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for another Account's night", res.StatusCode)
	}
}
