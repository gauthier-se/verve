package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// postLogin sends a login request from a fixed remote address and returns the
// raw response, so callers can inspect status, cookies, and body.
func postLogin(t *testing.T, srv *Server, email, password, remoteAddr string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

func sessionCookie(res *http.Response) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

func TestLoginSetsSecureSessionCookie(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res := postLogin(t, srv, testEmail, testPassword, "10.0.0.1:1234")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	c := sessionCookie(res)
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if !c.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Value == "" {
		t.Error("session cookie has empty value")
	}
}

func TestLoginWrongPasswordRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res := postLogin(t, srv, testEmail, "wrong password", "10.0.0.2:1")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
	if sessionCookie(res) != nil {
		t.Error("a rejected login must not set a session cookie")
	}
}

func TestLoginUnknownEmailRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res := postLogin(t, srv, "ghost@example.com", "whatever", "10.0.0.3:1")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}

func TestLoginValidationErrors(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res := postLogin(t, srv, "", "", "10.0.0.4:1")
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
}

func TestSeriesWithoutSessionRejected(t *testing.T) {
	srv, models, _ := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
	})
	// No cookie attached.
	res, _ := do(t, srv, "/v1/series?metric=steps&from=2024-01-01&to=2024-01-02")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}

func TestSeriesWithStaleCookieRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	stale := &http.Cookie{Name: sessionCookieName, Value: "not-a-real-token"}
	res, _ := do(t, srv, "/v1/series?metric=steps&range=7d", stale)
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an unknown token", res.StatusCode)
	}
}

func TestMeReturnsAuthenticatedAccount(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := do(t, srv, "/v1/auth/me", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var view accountView
	if err := json.Unmarshal(body["account"], &view); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if view.Email != testEmail {
		t.Errorf("email = %q, want %q", view.Email, testEmail)
	}
}

func TestMeWithoutSessionRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, _ := do(t, srv, "/v1/auth/me")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", res.StatusCode)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	// The session works before logout.
	if res, _ := do(t, srv, "/v1/auth/me", cookie); res.StatusCode != http.StatusOK {
		t.Fatalf("pre-logout /me status = %d, want 200", res.StatusCode)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec.Result().StatusCode)
	}

	// The same cookie is now dead server-side.
	if res, _ := do(t, srv, "/v1/auth/me", cookie); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("post-logout /me status = %d, want 401", res.StatusCode)
	}
}

// TestAccountsAreIsolated is the acceptance check: two accounts never see each
// other's data, and a session for one only ever returns that account's rows.
func TestAccountsAreIsolated(t *testing.T) {
	srv, models, aliceCookie := newTestServer(t) // dev@example.com is "alice" here
	seedAccountWithPassword(t, models, "bob@example.com", testPassword)

	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 111, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
	})
	seedSteps(t, models, "bob@example.com", []data.Measurement{
		{Metric: "steps", Value: 999, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "b"},
	})

	bobCookie := login(t, srv, "bob@example.com", testPassword)

	assertOnlyValue := func(cookie *http.Cookie, want float64) {
		t.Helper()
		_, body := do(t, srv, "/v1/series?metric=steps&from=2024-01-01&to=2024-01-02", cookie)
		var series query.Series
		if err := json.Unmarshal(body["series"], &series); err != nil {
			t.Fatalf("decode series: %v", err)
		}
		if len(series.Points) != 1 || series.Points[0].Value != want {
			t.Errorf("points = %+v, want a single bucket of %v", series.Points, want)
		}
	}
	assertOnlyValue(aliceCookie, 111)
	assertOnlyValue(bobCookie, 999)
}

func TestLoginIsRateLimited(t *testing.T) {
	srv, _, _ := newTestServer(t)
	const ip = "203.0.113.7:5000"
	// The limiter allows a burst of 5; the 6th rapid attempt from one IP is 429.
	var got429 bool
	for i := 0; i < 8; i++ {
		res := postLogin(t, srv, testEmail, "wrong password", ip)
		if res.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("rapid repeated logins from one IP were never rate-limited")
	}

	// A different IP is unaffected by the first IP's throttling.
	if res := postLogin(t, srv, testEmail, testPassword, "203.0.113.8:5000"); res.StatusCode != http.StatusOK {
		t.Errorf("fresh IP login status = %d, want 200 (not throttled)", res.StatusCode)
	}
}
