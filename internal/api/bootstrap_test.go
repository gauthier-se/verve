package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// postRegister sends a first-run bootstrap request from a fixed remote address
// and returns the raw response, so callers can inspect status, cookies, and body.
func postRegister(t *testing.T, srv *Server, email, password, remoteAddr string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

// authState GETs /v1/auth/state and returns the needs_bootstrap boolean.
func authState(t *testing.T, srv *Server) bool {
	t.Helper()
	res, body := do(t, srv, "/v1/auth/state")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("state status = %d, want 200", res.StatusCode)
	}
	var needs bool
	if err := json.Unmarshal(body["needs_bootstrap"], &needs); err != nil {
		t.Fatalf("decode needs_bootstrap: %v", err)
	}
	return needs
}

func TestAuthStateReflectsBootstrapNeed(t *testing.T) {
	srv, _ := newEmptyServer(t)

	if !authState(t, srv) {
		t.Error("needs_bootstrap = false on a fresh instance, want true")
	}

	if res := postRegister(t, srv, "admin@example.com", testPassword, "10.0.0.1:1"); res.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", res.StatusCode)
	}

	if authState(t, srv) {
		t.Error("needs_bootstrap = true after the first account exists, want false")
	}
}

func TestRegisterCreatesFirstAccountAndAutoLogsIn(t *testing.T) {
	srv, models := newEmptyServer(t)

	res := postRegister(t, srv, "admin@example.com", testPassword, "10.0.0.1:1")
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", res.StatusCode)
	}

	// Auto-login: the response carries a usable session cookie.
	c := sessionCookie(res)
	if c == nil {
		t.Fatal("register set no session cookie (no auto-login)")
	}
	if resMe, _ := do(t, srv, "/v1/auth/me", c); resMe.StatusCode != http.StatusOK {
		t.Errorf("post-register /me status = %d, want 200 (auto-login)", resMe.StatusCode)
	}

	// The account is seeded through the shared path (ADR 0018): one "Aperçu" board.
	acc, err := models.Accounts.GetByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	dashboards, err := models.Dashboards.ListByAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(dashboards) != 1 || dashboards[0].Name != "Aperçu" {
		t.Errorf("seeded dashboards = %+v, want one named Aperçu", dashboards)
	}
}

func TestRegisterClosedOnceInitialized(t *testing.T) {
	srv, _ := newEmptyServer(t)

	if res := postRegister(t, srv, "admin@example.com", testPassword, "10.0.0.1:1"); res.StatusCode != http.StatusCreated {
		t.Fatalf("first register status = %d, want 201", res.StatusCode)
	}

	// A second register — even with a different email — is refused server-side.
	res := postRegister(t, srv, "intruder@example.com", testPassword, "10.0.0.2:1")
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second register status = %d, want 409", res.StatusCode)
	}
	if sessionCookie(res) != nil {
		t.Error("a refused register must not set a session cookie")
	}
}

func TestRegisterValidationErrors(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res := postRegister(t, srv, "", "", "10.0.0.1:1")
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("register status = %d, want 422", res.StatusCode)
	}
}

func TestRegisterIsRateLimited(t *testing.T) {
	srv, _ := newEmptyServer(t)
	const ip = "203.0.113.9:5000"
	// The shared login limiter allows a burst of 5; a rapid run from one IP must
	// eventually 429 rather than letting an attacker hammer the endpoint.
	var got429 bool
	for i := 0; i < 8; i++ {
		// Empty body → 422 until the limiter trips; we only assert the throttle.
		res := postRegister(t, srv, "", "", ip)
		if res.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("rapid repeated register attempts from one IP were never rate-limited")
	}
}
