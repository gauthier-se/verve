package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
)

// The paths of the account operations on passwords and sessions.
const (
	passwordPath = "/api/v1/account/password" //nolint:gosec // G101: a path, not a credential
	sessionsPath = "/api/v1/account/sessions"
	mePath       = "/api/v1/auth/me"
)

// newPassword is the password that the tests change to.
const newPassword = "a brand new passphrase" //nolint:gosec // G101: a fake password of the tests

// The user agents of the sessions of the tests.
const (
	iPhoneSafari = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1"
	linuxFirefox = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
)

// accountTest is an authTest with alice, who has the password alicePassword, and bob, each with a
// session.
type accountTest struct {
	authTest

	alice, bob             store.User
	aliceCookie, bobCookie string
	handler                http.Handler
}

// newAccountTest returns an accountTest on a fresh store, at authTestNow.
func newAccountTest(t *testing.T) accountTest {
	t.Helper()

	a := accountTest{authTest: newAuthTest(t)}
	a.alice = a.mustCreateAlice(t)
	a.bob = a.mustCreateUser(t, "bob", store.RoleMember)
	a.aliceCookie = a.mustCreateCookie(t, a.alice)
	a.bobCookie = a.mustCreateCookie(t, a.bob)
	a.handler = a.authTest.handler(t, "")

	return a
}

// mustCreateCookie creates a password session of user and returns its session cookie, as
// name=value.
func (a authTest) mustCreateCookie(t *testing.T, user store.User) string {
	t.Helper()

	return "verve_session=" + a.mustCreateSession(t, user)
}

// mustCreateSessionFrom creates a password session of user from the client at clientIP with
// userAgent, and returns its session cookie, as name=value, and the session.
func (a authTest) mustCreateSessionFrom(t *testing.T, user store.User, clientIP, userAgent string) (string, store.Session) {
	t.Helper()

	id, session, err := a.sessions.Create(t.Context(), a.store, auth.NewSession{
		UserID:    user.ID,
		Method:    store.LoginPassword,
		ClientIP:  clientIP,
		UserAgent: userAgent,
	})
	require.NoError(t, err)

	return "verve_session=" + id, session
}

// accountRequest sends method target with body on h, an instance without a public URL, as the web
// app sends it from the phone, with the cookies, and returns the response.
func accountRequest(t *testing.T, h http.Handler, method, target, body string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	r := httptest.NewRequestWithContext(t.Context(), method, "http://example.com"+target, reader)
	r.RemoteAddr = phoneAddr
	r.Header.Set("User-Agent", userAgent)
	r.Header.Set("Verve-Csrf", "1")
	r.Header.Set("Origin", "http://example.com")

	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	return serveWith(h, r, cookies...)
}

// changePassword changes the password from current to next on h, with the cookies, and returns the
// response.
func changePassword(t *testing.T, h http.Handler, current, next string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]string{"currentPassword": current, "newPassword": next})
	require.NoError(t, err)

	return accountRequest(t, h, http.MethodPost, passwordPath, string(body), cookies...)
}

// requireAuthenticates checks that each of the cookies authenticates on h.
func requireAuthenticates(t *testing.T, h http.Handler, cookies ...string) {
	t.Helper()

	for _, cookie := range cookies {
		require.Equal(t, http.StatusOK, getWith(t, h, mePath, cookie).Code, cookie)
	}
}

// requireRevoked checks that none of the cookies authenticates on h.
func requireRevoked(t *testing.T, h http.Handler, cookies ...string) {
	t.Helper()

	for _, cookie := range cookies {
		require.Equal(t, http.StatusUnauthorized, getWith(t, h, mePath, cookie).Code, cookie)
	}
}

// Given the right current password and a valid new one, then POST /api/v1/account/password answers
// 204 and changes the password: the next login takes the new one only. Every other session of the
// user is revoked, the current one is kept, and the sessions of other users are kept.
func TestChangePassword_ChangesItAndRevokesTheOtherSessions(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	laptop := a.mustCreateCookie(t, a.alice)

	rec := changePassword(t, a.handler, alicePassword, newPassword, a.aliceCookie)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rec.Body.String())
	require.Empty(t, rec.Header().Values("Set-Cookie"))

	requireAuthenticates(t, a.handler, a.aliceCookie, a.bobCookie)
	requireRevoked(t, a.handler, laptop)

	requireInvalidCredentials(t, loginAt(t, a.handler, laptopAddr, "alice", alicePassword))
	require.Equal(t, http.StatusOK, loginAt(t, a.handler, laptopAddr, "alice", newPassword).Code)
}

// Given a wrong current password, then 403 invalid_credentials: the password and the other sessions
// are unchanged. The attempt counts in the login rate limit: after 5 of them, a change and a login
// of the user answer 429 rate_limited, even with the right password.
func TestChangePassword_WrongCurrentPasswordIsForbiddenAndRateLimited(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	laptop := a.mustCreateCookie(t, a.alice)

	for range 5 {
		requireProblem(t, changePassword(t, a.handler, wrongPassword, newPassword, a.aliceCookie),
			http.StatusForbidden, "invalid_credentials")
	}

	requireRateLimited(t, changePassword(t, a.handler, alicePassword, newPassword, a.aliceCookie), "1")
	requireRateLimited(t, changePassword(t, a.handler, alicePassword, "short", a.aliceCookie), "1")
	requireRateLimited(t, loginAt(t, a.handler, laptopAddr, "alice", alicePassword), "1")

	requireAuthenticates(t, a.handler, a.aliceCookie, laptop)

	a.clock.Advance(time.Second)
	require.Equal(t, http.StatusOK, loginAt(t, a.handler, laptopAddr, "alice", alicePassword).Code)
}

// Given a new password that breaks the password policy, then 422 with the code of the rule: the
// password and the other sessions are unchanged.
func TestChangePassword_WeakNewPasswordAnswersThePolicyCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		code     string
	}{
		{name: "too short", password: "short", code: "password_too_short"},
		{name: "too long", password: strings.Repeat("é", 129), code: "password_too_long"},
		{name: "too common", password: "PasswordPassword", code: "password_too_common"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAccountTest(t)
			laptop := a.mustCreateCookie(t, a.alice)

			requireProblem(t, changePassword(t, a.handler, alicePassword, tt.password, a.aliceCookie),
				http.StatusUnprocessableEntity, tt.code)

			requireAuthenticates(t, a.handler, a.aliceCookie, laptop)
			require.Equal(t, http.StatusOK, loginAt(t, a.handler, laptopAddr, "alice", alicePassword).Code)
		})
	}
}

// Given GET /api/v1/account/sessions, then it lists the valid sessions of the user, most recently
// seen first: each with its public id, the browser and device of its user agent, its client IP, its
// login method, its last seen time, and whether it is the session of the request. A session
// without a user agent or client IP leaves them out. The sessions of other users and the expired
// ones are not listed.
func TestListSessions_ListsTheSessionsOfTheUser(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	a.mustCreateSessionFrom(t, a.alice, "192.0.2.1", iPhoneSafari)

	// The sessions of newAccountTest, and the one above, are idle for longer than 30 days.
	a.clock.Advance(30*24*time.Hour + time.Minute)
	_, laptop := a.mustCreateSessionFrom(t, a.alice, "198.51.100.20", linuxFirefox)
	a.mustCreateSessionFrom(t, a.bob, "198.51.100.30", linuxFirefox)

	a.clock.Advance(time.Minute)
	_, unknown := a.mustCreateSessionFrom(t, a.alice, "", "")

	a.clock.Advance(time.Hour)
	current, phone := a.mustCreateSessionFrom(t, a.alice, "203.0.113.7", iPhoneSafari)

	rec := getWith(t, a.handler, sessionsPath, current)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, fmt.Sprintf(`{"items": [
		{
			"id": %q,
			"browser": "Safari",
			"device": "iPhone",
			"clientIp": "203.0.113.7",
			"method": "password",
			"lastSeenAt": "2026-12-04T09:02:00Z",
			"current": true
		},
		{
			"id": %q,
			"method": "password",
			"lastSeenAt": "2026-12-04T08:02:00Z",
			"current": false
		},
		{
			"id": %q,
			"browser": "Firefox",
			"device": "Linux",
			"clientIp": "198.51.100.20",
			"method": "password",
			"lastSeenAt": "2026-12-04T08:01:00Z",
			"current": false
		}
	]}`, phone.PublicID, unknown.PublicID, laptop.PublicID), rec.Body.String())
}

// Given DELETE /api/v1/account/sessions/{id} on a session of the user, then it answers 204 and the
// session is revoked, the others kept. Given the session of another user, or an unknown id, then
// 404 not_found, and the session is kept.
func TestRevokeSession_RevokesASessionOfTheUserOnly(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	laptop, laptopSession := a.mustCreateSessionFrom(t, a.alice, "198.51.100.20", linuxFirefox)
	tablet := a.mustCreateCookie(t, a.alice)
	bobLaptop, bobSession := a.mustCreateSessionFrom(t, a.bob, "198.51.100.30", linuxFirefox)

	rec := accountRequest(t, a.handler, http.MethodDelete, sessionsPath+"/"+laptopSession.PublicID, "", a.aliceCookie)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rec.Body.String())
	requireRevoked(t, a.handler, laptop)
	requireAuthenticates(t, a.handler, a.aliceCookie, tablet)

	for _, id := range []string{bobSession.PublicID, laptopSession.PublicID, "unknown"} {
		requireProblem(t, accountRequest(t, a.handler, http.MethodDelete, sessionsPath+"/"+id, "", a.aliceCookie),
			http.StatusNotFound, "not_found")
	}

	requireAuthenticates(t, a.handler, bobLaptop, a.bobCookie)
}

// Given DELETE /api/v1/account/sessions, then it answers 204 and every session of the user but the
// current one is revoked. The sessions of other users are kept.
func TestRevokeOtherSessions_KeepsTheCurrentOne(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	laptop := a.mustCreateCookie(t, a.alice)
	tablet := a.mustCreateCookie(t, a.alice)

	rec := accountRequest(t, a.handler, http.MethodDelete, sessionsPath, "", a.aliceCookie)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rec.Body.String())
	requireRevoked(t, a.handler, laptop, tablet)
	requireAuthenticates(t, a.handler, a.aliceCookie, a.bobCookie)
}

// Given no session, then the password and sessions operations answer 401 unauthorized.
func TestAccount_PasswordAndSessionsRequireASession(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)

	for _, rec := range []*httptest.ResponseRecorder{
		changePassword(t, a.handler, alicePassword, newPassword),
		getWith(t, a.handler, sessionsPath),
		accountRequest(t, a.handler, http.MethodDelete, sessionsPath+"/unknown", ""),
		accountRequest(t, a.handler, http.MethodDelete, sessionsPath, ""),
	} {
		requireProblem(t, rec, http.StatusUnauthorized, "unauthorized")
	}
}

// Given a password change, the revocation of a session and of the other sessions, then each records
// its audit event with the user as actor and the client of the request: the revocations with the
// number of sessions revoked. A revocation of the other sessions when there are none, and a wrong
// current password, record nothing.
func TestAccount_RecordsAuditEvents(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	_, laptop := a.mustCreateSessionFrom(t, a.alice, "198.51.100.20", linuxFirefox)
	a.mustCreateCookie(t, a.alice)
	a.mustCreateCookie(t, a.alice)

	require.Equal(t, http.StatusNoContent,
		accountRequest(t, a.handler, http.MethodDelete, sessionsPath+"/"+laptop.PublicID, "", a.aliceCookie).Code)
	require.Equal(t, http.StatusNoContent, accountRequest(t, a.handler, http.MethodDelete, sessionsPath, "", a.aliceCookie).Code)
	require.Equal(t, http.StatusNoContent, accountRequest(t, a.handler, http.MethodDelete, sessionsPath, "", a.aliceCookie).Code)
	require.Equal(t, http.StatusForbidden, changePassword(t, a.handler, wrongPassword, newPassword, a.aliceCookie).Code)
	require.Equal(t, http.StatusNoContent, changePassword(t, a.handler, alicePassword, newPassword, a.aliceCookie).Code)

	require.Equal(t, []recordedEvent{
		{Type: "auth.session_revoked", ActorID: a.alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{"revoked":1}`},
		{Type: "auth.session_revoked", ActorID: a.alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{"revoked":2}`},
		{Type: "auth.password_changed", ActorID: a.alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{}`},
	}, a.recordedEvents(t))
}
