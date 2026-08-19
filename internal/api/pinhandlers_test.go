package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// listPins is a test helper returning the Account's Pins in sidebar order.
func listPins(t *testing.T, srv *Server, cookie *http.Cookie) []pinView {
	t.Helper()
	res, body := doReq(t, srv, http.MethodGet, "/v1/pins", "", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list pins status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var pins []pinView
	if err := json.Unmarshal(body["pins"], &pins); err != nil {
		t.Fatalf("decode pins: %v", err)
	}
	return pins
}

// pin is a test helper pinning one Metric and asserting the call succeeded.
func pin(t *testing.T, srv *Server, cookie *http.Cookie, metric string) {
	t.Helper()
	res, body := doReq(t, srv, http.MethodPost, "/v1/pins", `{"metric":"`+metric+`"}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pin %s status = %d, want 200 (%s)", metric, res.StatusCode, body["error"])
	}
}

func TestPinsRequireAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, "/v1/pins", ""},
		{http.MethodPost, "/v1/pins", `{"metric":"body_mass"}`},
		{http.MethodDelete, "/v1/pins/body_mass", ""},
	} {
		res, _ := doReq(t, srv, tc.method, tc.target, tc.body)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s %s status = %d, want 401", tc.method, tc.target, res.StatusCode)
		}
	}
}

func TestListPinsEmpty(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	// A new Account has no Pins: ADR 0018 seeds a Dashboard because an empty app has
	// no next step, which is not true of an empty sidebar section.
	if pins := listPins(t, srv, cookie); len(pins) != 0 {
		t.Errorf("new account has %d pins, want 0", len(pins))
	}
}

func TestPinAndListInInsertionOrder(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	pin(t, srv, cookie, "body_mass")
	pin(t, srv, cookie, "steps")
	pin(t, srv, cookie, "resting_heart_rate")

	pins := listPins(t, srv, cookie)
	want := []string{"body_mass", "steps", "resting_heart_rate"}
	if len(pins) != len(want) {
		t.Fatalf("got %d pins, want %d", len(pins), len(want))
	}
	for i, metric := range want {
		if pins[i].Metric != metric {
			t.Errorf("pin %d = %q, want %q", i, pins[i].Metric, metric)
		}
		if pins[i].Position != i {
			t.Errorf("pin %q position = %d, want %d", metric, pins[i].Position, i)
		}
	}
}

func TestPinIsIdempotent(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	pin(t, srv, cookie, "body_mass")
	pin(t, srv, cookie, "steps")
	// Pinning again must not duplicate the row nor move it to the end: the toggle
	// asks for a state, and that state already holds.
	pin(t, srv, cookie, "body_mass")

	pins := listPins(t, srv, cookie)
	if len(pins) != 2 {
		t.Fatalf("got %d pins after re-pinning, want 2", len(pins))
	}
	if pins[0].Metric != "body_mass" {
		t.Errorf("first pin = %q, want body_mass (position must not shift)", pins[0].Metric)
	}
}

func TestUnpin(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	pin(t, srv, cookie, "body_mass")
	pin(t, srv, cookie, "steps")

	res, _ := doReq(t, srv, http.MethodDelete, "/v1/pins/body_mass", "", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("unpin status = %d, want 204", res.StatusCode)
	}
	pins := listPins(t, srv, cookie)
	if len(pins) != 1 || pins[0].Metric != "steps" {
		t.Errorf("pins after unpin = %+v, want only steps", pins)
	}
}

func TestUnpinAbsentIsNoContent(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	// Same reasoning as the idempotent create: the requested state already holds.
	res, _ := doReq(t, srv, http.MethodDelete, "/v1/pins/body_mass", "", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("unpin absent status = %d, want 204", res.StatusCode)
	}
}

func TestPinRejectsUnknownMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	for _, body := range []string{`{"metric":"not_a_metric"}`, `{"metric":""}`, `{}`} {
		res, _ := doReq(t, srv, http.MethodPost, "/v1/pins", body, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("pin %s status = %d, want 422", body, res.StatusCode)
		}
	}
	if pins := listPins(t, srv, cookie); len(pins) != 0 {
		t.Errorf("rejected pins left %d rows, want 0", len(pins))
	}
}

func TestPinIsolationAcrossAccounts(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	pin(t, srv, cookie, "body_mass")

	seedAccountWithPassword(t, models, "bob@example.com", testPassword)
	bobCookie := login(t, srv, "bob@example.com", testPassword)

	// Bob does not see Alice's Pin.
	if pins := listPins(t, srv, bobCookie); len(pins) != 0 {
		t.Errorf("bob sees %d pins, want 0", len(pins))
	}
	// And unpinning the same Metric as Bob leaves Alice's row untouched: the delete
	// is scoped by Account, so it silently matches nothing.
	res, _ := doReq(t, srv, http.MethodDelete, "/v1/pins/body_mass", "", bobCookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("bob unpin status = %d, want 204", res.StatusCode)
	}
	if pins := listPins(t, srv, cookie); len(pins) != 1 {
		t.Errorf("alice has %d pins after bob's delete, want 1", len(pins))
	}
}
