package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// The paths of the login operations.
const (
	loginPath  = "/api/v1/auth/login"
	logoutPath = "/api/v1/auth/logout"
)

// The passwords of the login tests.
const (
	alicePassword = "correct horse battery staple"
	wrongPassword = "Tr0ub4dor&3" //nolint:gosec // G101: a fake password of the tests
)

// The client addresses of the login tests, and the user agent of their requests.
const (
	phoneAddr  = "203.0.113.7:51000"
	laptopAddr = "198.51.100.20:52000"
	userAgent  = "Mozilla/5.0 (iPhone)"
)

// The problems of a failed login and of a delayed attempt.
const (
	invalidCredentialsBody = `{"type":"about:blank","title":"Unauthorized","status":401,"code":"invalid_credentials"}` //nolint:gosec // G101: an error code, not a credential
	rateLimitedBody        = `{"type":"about:blank","title":"Too Many Requests","status":429,"code":"rate_limited"}`
)

// mustCreateAlice stores alice, an active member with an email at example.com and the password
// alicePassword.
func (a authTest) mustCreateAlice(t *testing.T) store.User {
	t.Helper()

	user, err := a.store.Users().Create(t.Context(), store.NewUser{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: testHasher.Hash(alicePassword),
		Role:         store.RoleMember,
	})
	require.NoError(t, err)

	return user
}

// sameOriginPost returns a POST of body to target as the web app at origin sends it: with the CSRF
// header and that Origin, from the client at remoteAddr.
func sameOriginPost(t *testing.T, origin, target, body, remoteAddr string) *http.Request {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, origin+target, strings.NewReader(body))
	r.RemoteAddr = remoteAddr
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", userAgent)
	r.Header.Set("Verve-Csrf", "1")
	r.Header.Set("Origin", origin)

	return r
}

// loginBody returns the body of a login as login with password.
func loginBody(t *testing.T, login, password string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{"login": login, "password": password})
	require.NoError(t, err)

	return string(body)
}

// loginAt logs in on h, an instance without a public URL, as login with password, from the client at
// remoteAddr with the cookies, and returns the response.
func loginAt(t *testing.T, h http.Handler, remoteAddr, login, password string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, sameOriginPost(t, "http://example.com", loginPath, loginBody(t, login, password), remoteAddr), cookies...)
}

// logoutAt logs out on h, an instance at origin, from the phone with the cookies, and returns the
// response.
func logoutAt(t *testing.T, h http.Handler, origin string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, sameOriginPost(t, origin, logoutPath, "", phoneAddr), cookies...)
}

// requireInvalidCredentials checks that rec is the 401 problem of a failed login, without cookie.
func requireInvalidCredentials(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, invalidCredentialsBody, rec.Body.String())
	require.Empty(t, rec.Header().Values("Set-Cookie"))
	require.Empty(t, rec.Header().Values("Retry-After"))
}

// requireRateLimited checks that rec is the 429 problem of a delayed attempt, whose Retry-After is
// retryAfter seconds, without cookie.
func requireRateLimited(t *testing.T, rec *httptest.ResponseRecorder, retryAfter string) {
	t.Helper()

	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, rateLimitedBody, rec.Body.String())
	require.Equal(t, []string{retryAfter}, rec.Header().Values("Retry-After"))
	require.Empty(t, rec.Header().Values("Set-Cookie"))
}

// sessionCookieOf returns the value of the session cookie named name that rec sets.
func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder, name string) string {
	t.Helper()

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, name, cookies[0].Name)
	require.NotEmpty(t, cookies[0].Value)

	return cookies[0].Value
}

// Given valid credentials, by username or by email in any case, then POST /api/v1/auth/login
// answers 200 with the user, sets the session cookie of the public URL, which then authenticates,
// with the expiry of the session, and updates the last login time of the user.
func TestLogin_ValidCredentialsSetTheSessionCookie(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		publicURL  string
		origin     string
		login      string
		cookieName string
		// attributes follow the identifier in the cookie.
		attributes string
	}{
		{
			name:       "username",
			origin:     "http://example.com",
			login:      "alice",
			cookieName: "verve_session",
			attributes: "; Path=/; Expires=Fri, 04 Dec 2026 09:00:00 GMT; HttpOnly; SameSite=Lax",
		},
		{
			name:       "email in another case",
			origin:     "http://example.com",
			login:      "Alice@Example.com",
			cookieName: "verve_session",
			attributes: "; Path=/; Expires=Fri, 04 Dec 2026 09:00:00 GMT; HttpOnly; SameSite=Lax",
		},
		{
			name:       "https public URL",
			publicURL:  "https://verve.example.com",
			origin:     "https://verve.example.com",
			login:      "ALICE",
			cookieName: "__Host-verve_session",
			attributes: "; Path=/; Expires=Fri, 04 Dec 2026 09:00:00 GMT; HttpOnly; Secure; SameSite=Lax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			alice := a.mustCreateAlice(t)
			a.clock.Advance(time.Hour)
			h := a.handler(t, tt.publicURL)

			rec := serveWith(h, sameOriginPost(t, tt.origin, loginPath, loginBody(t, tt.login, alicePassword), phoneAddr))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			require.JSONEq(t, fmt.Sprintf(`{"id":%d,"username":"alice","email":"alice@example.com","role":"member"}`,
				alice.ID), rec.Body.String())

			id := sessionCookieOf(t, rec, tt.cookieName)
			require.Equal(t, []string{tt.cookieName + "=" + id + tt.attributes}, rec.Header().Values("Set-Cookie"))

			me := getWith(t, h, "/api/v1/auth/me", tt.cookieName+"="+id)
			require.Equal(t, http.StatusOK, me.Code)

			stored, err := a.store.Users().Get(t.Context(), alice.ID)
			require.NoError(t, err)
			require.Equal(t, authTestNow.Add(time.Hour), stored.LastLoginAt)
		})
	}
}

// disabledLoginsStore wraps a real store, so that the users it finds by login are disabled. No
// repository changes the status of a user yet.
type disabledLoginsStore struct {
	store.Store
}

func (s disabledLoginsStore) Users() store.UsersRepository {
	return disabledLoginUsers{UsersRepository: s.Store.Users()}
}

type disabledLoginUsers struct {
	store.UsersRepository
}

func (u disabledLoginUsers) GetByLogin(ctx context.Context, login string) (store.User, error) {
	user, err := u.UsersRepository.GetByLogin(ctx, login)
	user.Status = store.UserDisabled

	return user, err
}

// Given a wrong password, an unknown login or a disabled account, then the same 401
// invalid_credentials with the same body, and no cookie.
func TestLogin_FailuresAnswerTheSameProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		login    string
		password string
		disabled bool
	}{
		{name: "wrong password", login: "alice", password: wrongPassword},
		{name: "unknown login", login: "bob", password: alicePassword},
		{name: "disabled account", login: "alice", password: alicePassword, disabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			a.mustCreateAlice(t)

			deps := a.deps(t, "")
			if tt.disabled {
				deps.Store = disabledLoginsStore{Store: a.store}
			}

			requireInvalidCredentials(t, loginAt(t, NewHandler(deps), phoneAddr, tt.login, tt.password))
		})
	}
}

// Given 5 failures for one login, whether or not a user has it, then the next attempt answers 429
// rate_limited with Retry-After: 1, from any client IP and even with the right password.
func TestLogin_FiveFailuresForALoginAreRateLimited(t *testing.T) {
	t.Parallel()

	for _, login := range []string{"alice", "nobody"} {
		t.Run(login, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			a.mustCreateAlice(t)
			h := a.handler(t, "")

			for range 5 {
				requireInvalidCredentials(t, loginAt(t, h, phoneAddr, login, wrongPassword))
			}

			requireRateLimited(t, loginAt(t, h, laptopAddr, login, alicePassword), "1")
		})
	}
}

// Given failures made as soon as their delay passed, then Retry-After doubles, in whole seconds
// rounded up.
func TestLogin_RetryAfterDoublesInWholeSeconds(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")

	for range 5 {
		requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "alice", wrongPassword))
	}

	requireRateLimited(t, loginAt(t, h, phoneAddr, "alice", wrongPassword), "1")

	a.clock.Advance(time.Second)
	requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "alice", wrongPassword))
	requireRateLimited(t, loginAt(t, h, phoneAddr, "alice", wrongPassword), "2")

	a.clock.Advance(500 * time.Millisecond)
	requireRateLimited(t, loginAt(t, h, phoneAddr, "alice", wrongPassword), "2")

	a.clock.Advance(1500 * time.Millisecond)
	requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "alice", wrongPassword))
	requireRateLimited(t, loginAt(t, h, phoneAddr, "alice", wrongPassword), "4")
}

// Given 20 failures from one client IP, each for another login, then the next attempt from that IP
// answers 429 rate_limited, whatever the login. The other IPs are not delayed.
func TestLogin_TwentyFailuresFromAnIPAreRateLimited(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	a.mustCreateAlice(t)
	h := a.handler(t, "")

	for i := range 20 {
		requireInvalidCredentials(t, loginAt(t, h, phoneAddr, fmt.Sprintf("user%d", i), wrongPassword))
	}

	requireRateLimited(t, loginAt(t, h, phoneAddr, "alice", alicePassword), "1")

	rec := loginAt(t, h, laptopAddr, "alice", alicePassword)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// Given a delayed login, when its delay has passed, then an attempt is accepted again. Given a
// success, then the failures of the login start over: 5 more failures are needed to delay it.
func TestLogin_DelayPassedAcceptsAttemptsAndSuccessResetsTheLogin(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	a.mustCreateAlice(t)
	h := a.handler(t, "")

	for range 5 {
		requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "alice", wrongPassword))
	}

	a.clock.Advance(time.Second)

	rec := loginAt(t, h, laptopAddr, "alice", alicePassword)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for range 5 {
		requireInvalidCredentials(t, loginAt(t, h, laptopAddr, "alice", wrongPassword))
	}

	requireRateLimited(t, loginAt(t, h, laptopAddr, "alice", alicePassword), "1")
}

// Given a password of 128 characters, whatever their bytes, then the login runs. Given a longer
// password or an empty login, then 422 validation_failed, which counts no failure. Given a body
// larger than 8 KiB, then 413.
func TestLogin_PasswordIsAtMost128Characters(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	a.mustCreateAlice(t)
	h := a.handler(t, "")

	requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "alice", strings.Repeat("é", 128)))

	for range 5 {
		rec := loginAt(t, h, phoneAddr, "alice", strings.Repeat("a", 129))
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Contains(t, rec.Body.String(), `"location":"body.password"`)

		rec = loginAt(t, h, phoneAddr, "", alicePassword)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Contains(t, rec.Body.String(), `"location":"body.login"`)
	}

	rec := loginAt(t, h, phoneAddr, "alice", alicePassword)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	large := `{"login":"alice","password":"` + alicePassword + `","padding":"` + strings.Repeat("a", 8*1024) + `"}`
	rec = serveWith(h, sameOriginPost(t, "http://example.com", loginPath, large, phoneAddr))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// Given a request that already has a session, then the login replaces it: the cookie holds a new
// identifier, and the former session no longer authenticates. A cookie of an unknown session does
// not fail the login.
func TestLogin_ReplacesTheSessionOfTheRequest(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	alice := a.mustCreateAlice(t)
	former := a.mustCreateSession(t, alice)
	h := a.handler(t, "")

	rec := loginAt(t, h, phoneAddr, "alice", alicePassword, "verve_session="+former)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	id := sessionCookieOf(t, rec, "verve_session")
	require.NotEqual(t, former, id)

	require.Equal(t, http.StatusUnauthorized, getWith(t, h, "/api/v1/auth/me", "verve_session="+former).Code)
	require.Equal(t, http.StatusOK, getWith(t, h, "/api/v1/auth/me", "verve_session="+id).Code)

	rec = loginAt(t, h, phoneAddr, "alice", alicePassword, "verve_session=bm90LWEtc2Vzc2lvbg")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// Given POST /api/v1/auth/logout with a session, then the answer is 204, the cookie of the public
// URL is cleared, and the session no longer authenticates.
func TestLogout_DeletesTheSessionAndClearsTheCookie(t *testing.T) {
	t.Parallel()

	tests := []struct {
		publicURL  string
		origin     string
		cookieName string
		cleared    string
	}{
		{
			origin:     "http://example.com",
			cookieName: "verve_session",
			cleared:    "verve_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
		},
		{
			publicURL:  "https://verve.example.com",
			origin:     "https://verve.example.com",
			cookieName: "__Host-verve_session",
			cleared:    "__Host-verve_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cookieName, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			id := a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))
			h := a.handler(t, tt.publicURL)

			rec := logoutAt(t, h, tt.origin, tt.cookieName+"="+id)

			require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
			require.Empty(t, rec.Body.String())
			require.Equal(t, []string{tt.cleared}, rec.Header().Values("Set-Cookie"))

			require.Equal(t, http.StatusUnauthorized, getWith(t, h, "/api/v1/auth/me", tt.cookieName+"="+id).Code)
		})
	}
}

// Given POST /api/v1/auth/logout without a session, or with an unknown, expired or revoked one,
// then the answer is still 204 with the cookie cleared.
func TestLogout_WithoutASessionStillAnswers204(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// cookie returns the session cookie to send, as name=value, or "" for none.
		cookie func(t *testing.T, a authTest) string
	}{
		{name: "missing", cookie: func(*testing.T, authTest) string { return "" }},
		{name: "unknown", cookie: func(*testing.T, authTest) string { return "verve_session=bm90LWEtc2Vzc2lvbg" }},
		{
			name: "expired",
			cookie: func(t *testing.T, a authTest) string {
				id := a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))
				a.clock.Advance(31 * 24 * time.Hour)

				return "verve_session=" + id
			},
		},
		{
			name: "revoked",
			cookie: func(t *testing.T, a authTest) string {
				id := a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))
				require.NoError(t, a.sessions.Revoke(t.Context(), a.store, id))

				return "verve_session=" + id
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)

			var cookies []string
			if c := tt.cookie(t, a); c != "" {
				cookies = append(cookies, c)
			}

			rec := logoutAt(t, a.handler(t, ""), "http://example.com", cookies...)

			require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
			require.Equal(t, []string{"verve_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"},
				rec.Header().Values("Set-Cookie"))
		})
	}
}

// recordedEvent is an audit event, as the login tests check it.
type recordedEvent struct {
	Type      string
	ActorID   int64
	SubjectID int64
	ClientIP  string
	UserAgent string
	Payload   string
}

// recordedEvents returns the events of the audit log of a, oldest first.
func (a authTest) recordedEvents(t *testing.T) []recordedEvent {
	t.Helper()

	events, err := a.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 100})
	require.NoError(t, err)

	recorded := make([]recordedEvent, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		recorded = append(recorded, recordedEvent{
			Type:      e.Type,
			ActorID:   e.ActorID,
			SubjectID: e.SubjectID,
			ClientIP:  e.ClientIP,
			UserAgent: e.UserAgent,
			Payload:   string(e.Payload),
		})
	}

	return recorded
}

// Given failed logins, a successful one and a logout, then each records its audit event with the
// client of the request: a failure with the attempted login, and the user of that login as subject
// unless no user has it, the success and the logout with the user as actor. A logout without a
// session records nothing.
func TestLogin_RecordsAuditEvents(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	alice := a.mustCreateAlice(t)
	h := a.handler(t, "")

	requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "Alice", wrongPassword))
	requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "nobody", wrongPassword))

	rec := loginAt(t, h, phoneAddr, "alice", alicePassword)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	id := sessionCookieOf(t, rec, "verve_session")

	require.Equal(t, http.StatusNoContent, logoutAt(t, h, "http://example.com", "verve_session="+id).Code)
	require.Equal(t, http.StatusNoContent, logoutAt(t, h, "http://example.com").Code)

	require.Equal(t, []recordedEvent{
		{Type: "auth.login_failed", SubjectID: alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{"attempted":"Alice"}`},
		{Type: "auth.login_failed", ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{"attempted":"nobody"}`},
		{Type: "auth.login_succeeded", ActorID: alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{"method":"password"}`},
		{Type: "auth.logout", ActorID: alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{}`},
	}, a.recordedEvents(t))
}

// Given a burst of failed logins from one client IP on logins that no user has, then a failure on
// the account of alice from that IP is not aggregated into the burst: alice sees it in her audit
// events, in a row of its own, and no row of hers counts the attempts on other logins.
func TestLogin_FailedLoginOfAnAccountStaysOutOfTheBurstsOfOtherLogins(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	alice := a.mustCreateAlice(t)
	aliceCookie := "verve_session=" + a.mustCreateSession(t, alice)
	h := a.handler(t, "")

	for i := range 20 {
		requireInvalidCredentials(t, loginAt(t, h, phoneAddr, fmt.Sprintf("nobody%d", i), wrongPassword))
	}

	// The 20 failures delayed the client IP for a second, and filled the burst of its row.
	a.clock.Advance(time.Second)
	requireInvalidCredentials(t, loginAt(t, h, phoneAddr, "alice", wrongPassword))

	rec := getWith(t, h, accountAuditPath, aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"items": [{
		"id": 21,
		"occurredAt": "2026-11-04T08:00:01Z",
		"type": "auth.login_failed",
		"subject": {"id": %d, "username": "alice", "deleted": false},
		"clientIp": "203.0.113.7",
		"userAgent": %q,
		"payload": {"attempted": "alice"},
		"count": 1,
		"lastAt": "2026-11-04T08:00:01Z"
	}]}`, alice.ID, userAgent), rec.Body.String())
}
