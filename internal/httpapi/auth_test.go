package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// authTestNow is when the sessions of the tests are created.
var authTestNow = time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)

// testHasher hashes the passwords of the tests with the lowest argon2id parameters, so that the
// tests that fail many logins stay fast.
var testHasher = auth.Hasher{Memory: 8, Iterations: 1, Parallelism: 1}

// unauthorizedBody is the problem of a request without a valid session.
const unauthorizedBody = `{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}`

// authTest holds a store on a migrated database, the sessions service and the fake clock of both.
type authTest struct {
	store    store.Store
	sessions auth.Sessions
	clock    *clock.Fake
}

// newAuthTest returns an authTest on a fresh temporary directory, at authTestNow. The store is
// closed at the end of the test.
func newAuthTest(t *testing.T) authTest {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	clk := clock.NewFake(authTestNow)
	s := sqlite.New(db, clk)

	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return authTest{store: s, sessions: auth.NewSessions(clk), clock: clk}
}

// deps returns the dependencies of a server on the store of a, whose public URL is publicURL, or
// unset when publicURL is empty.
func (a authTest) deps(t *testing.T, publicURL string) Deps {
	t.Helper()

	return Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Clock:     a.clock,
		Store:     a.store,
		PublicURL: mustPublicURL(t, publicURL),
		Hasher:    testHasher,
	}
}

// handler returns the handler of a server on the store of a, whose public URL is publicURL, or
// unset when publicURL is empty.
func (a authTest) handler(t *testing.T, publicURL string) http.Handler {
	t.Helper()

	return NewHandler(a.deps(t, publicURL))
}

// mustCreateUser stores an active user named username, with an email at example.com.
func (a authTest) mustCreateUser(t *testing.T, username string, role store.UserRole) store.User {
	t.Helper()

	user, err := a.store.Users().Create(t.Context(), store.NewUser{
		Username: username,
		Email:    username + "@example.com",
		Role:     role,
	})
	require.NoError(t, err)

	return user
}

// mustCreateSession creates a password session of user and returns its identifier.
func (a authTest) mustCreateSession(t *testing.T, user store.User) string {
	t.Helper()

	id, _, err := a.sessions.Create(t.Context(), a.store, auth.NewSession{UserID: user.ID, Method: store.LoginPassword})
	require.NoError(t, err)

	return id
}

// mustPublicURL parses value as a public URL, or returns the unset public URL when value is empty.
func mustPublicURL(t *testing.T, value string) config.PublicURL {
	t.Helper()

	var p config.PublicURL
	if value != "" {
		require.NoError(t, p.UnmarshalText([]byte(value)))
	}

	return p
}

// serveWith serves r on h with the cookies, each as name=value, and returns the response.
func serveWith(h http.Handler, r *http.Request, cookies ...string) *httptest.ResponseRecorder {
	for _, c := range cookies {
		r.Header.Add("Cookie", c)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	return rec
}

// getWith serves GET target on h with the cookies, each as name=value, and returns the response.
func getWith(t *testing.T, h http.Handler, target string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil), cookies...)
}

// Given a valid session cookie, then GET /api/v1/auth/me returns 200 with the id, username, email
// and role of its user.
func TestAuth_MeReturnsTheUserOfTheSession(t *testing.T) {
	t.Parallel()

	for _, role := range []store.UserRole{store.RoleMember, store.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			user := a.mustCreateUser(t, "alice", role)
			id := a.mustCreateSession(t, user)

			rec := getWith(t, a.handler(t, ""), "/api/v1/auth/me", "verve_session="+id)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			require.JSONEq(t, fmt.Sprintf(`{"id":%d,"username":"alice","email":"alice@example.com","role":%q}`,
				user.ID, role), rec.Body.String())
		})
	}
}

// Given a user without an email, then GET /api/v1/auth/me leaves the email out.
func TestAuth_MeLeavesOutAMissingEmail(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)

	user, err := a.store.Users().Create(t.Context(), store.NewUser{Username: "bob", Role: store.RoleMember})
	require.NoError(t, err)

	rec := getWith(t, a.handler(t, ""), "/api/v1/auth/me",
		"verve_session="+a.mustCreateSession(t, user))

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"username":"bob","role":"member"}`, user.ID), rec.Body.String())
}

// Given a missing, empty, unknown, expired or revoked session cookie on a protected operation, then
// 401 unauthorized.
func TestAuth_InvalidSessionReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// cookie returns the session cookie to send, as name=value, or "" for none.
		cookie func(t *testing.T, a authTest) string
	}{
		{
			name:   "missing",
			cookie: func(*testing.T, authTest) string { return "" },
		},
		{
			name: "empty",
			cookie: func(*testing.T, authTest) string {
				return "verve_session="
			},
		},
		{
			name: "unknown",
			cookie: func(*testing.T, authTest) string {
				return "verve_session=bm90LWEtc2Vzc2lvbg"
			},
		},
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

			rec := getWith(t, a.handler(t, ""), "/api/v1/auth/me", cookies...)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			require.JSONEq(t, unauthorizedBody, rec.Body.String())
		})
	}
}

// Given a request with Authorization: Bearer, then its session cookie is ignored: the CSRF checks
// do not apply to it, so a cookie must not authenticate it.
func TestAuth_BearerRequestIgnoresTheSessionCookie(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	id := a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer vrv_unknown")

	rec := serveWith(a.handler(t, ""), r, "verve_session="+id)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, unauthorizedBody, rec.Body.String())
}

// adminAPIHandler returns the handler of a server on the store of a, with a test-only operation
// GET /api/v1/test/admin that requires an admin and answers 204.
func adminAPIHandler(t *testing.T, a authTest) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	api := NewAPI(mux, APIDeps{Store: a.store})
	huma.Register(api, huma.Operation{
		OperationID: "test-admin",
		Method:      http.MethodGet,
		Path:        "/api/v1/test/admin",
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		_, err := requireAdmin(ctx)

		return nil, err
	})

	return newHandler(a.deps(t, ""), mux)
}

// Given a member on an admin operation, then 403 forbidden. Given an admin, then the operation
// runs. Given no session, then 401 unauthorized.
func TestAuth_AdminOperationRequiresAnAdmin(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := adminAPIHandler(t, a)

	member := "verve_session=" + a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))
	admin := "verve_session=" + a.mustCreateSession(t, a.mustCreateUser(t, "root", store.RoleAdmin))

	rec := getWith(t, h, "/api/v1/test/admin", member)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`, rec.Body.String())

	rec = getWith(t, h, "/api/v1/test/admin", admin)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = getWith(t, h, "/api/v1/test/admin")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, unauthorizedBody, rec.Body.String())
}

// Given a valid session, then the operations find its principal in their context: the user id,
// the role and the session method.
func TestAuth_PrincipalIsInTheContext(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	user := a.mustCreateUser(t, "alice", store.RoleAdmin)
	id := a.mustCreateSession(t, user)

	var (
		got   auth.Principal
		found bool
	)

	h := newHandler(a.deps(t, ""), http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, found = PrincipalFrom(r.Context())
	}))

	getWith(t, h, "/api/v1/anything", "verve_session="+id)

	require.True(t, found)
	require.Equal(t, auth.Principal{UserID: user.ID, Role: store.RoleAdmin, Method: auth.MethodSession}, got)

	getWith(t, h, "/api/v1/anything")

	require.False(t, found)
	require.Equal(t, auth.Principal{}, got)
}

// Given an https public URL, then the session cookie is __Host-verve_session with HttpOnly,
// SameSite=Lax, Path=/ and Secure. Given http, or no public URL, then it is verve_session, without
// Secure.
func TestAuth_SessionCookieAttributes(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 12, 4, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		publicURL string
		want      string
	}{
		{
			publicURL: "https://verve.example.com",
			want:      "__Host-verve_session=c2Vzc2lvbg; Path=/; Expires=Fri, 04 Dec 2026 08:00:00 GMT; HttpOnly; Secure; SameSite=Lax",
		},
		{
			publicURL: "http://192.168.1.10:8080",
			want:      "verve_session=c2Vzc2lvbg; Path=/; Expires=Fri, 04 Dec 2026 08:00:00 GMT; HttpOnly; SameSite=Lax",
		},
		{
			publicURL: "",
			want:      "verve_session=c2Vzc2lvbg; Path=/; Expires=Fri, 04 Dec 2026 08:00:00 GMT; HttpOnly; SameSite=Lax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.publicURL, func(t *testing.T) {
			t.Parallel()

			cookie := newSessionCookie(mustPublicURL(t, tt.publicURL), "c2Vzc2lvbg", expiresAt)

			require.NoError(t, cookie.Valid())
			require.Equal(t, tt.want, cookie.String())
		})
	}
}

// Given an https public URL, then only the __Host-verve_session cookie authenticates: a cookie
// without the prefix may have been set by another host of the domain. Given http, then only
// verve_session does.
func TestAuth_ReadsTheCookieOfThePublicURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		publicURL  string
		cookieName string
		want       int
	}{
		{publicURL: "https://verve.example.com", cookieName: "__Host-verve_session", want: http.StatusOK},
		{publicURL: "https://verve.example.com", cookieName: "verve_session", want: http.StatusUnauthorized},
		{publicURL: "http://192.168.1.10:8080", cookieName: "verve_session", want: http.StatusOK},
		{publicURL: "http://192.168.1.10:8080", cookieName: "__Host-verve_session", want: http.StatusUnauthorized},
		{publicURL: "", cookieName: "verve_session", want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.publicURL+" "+tt.cookieName, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			id := a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))

			rec := getWith(t, a.handler(t, tt.publicURL), "/api/v1/auth/me", tt.cookieName+"="+id)

			require.Equal(t, tt.want, rec.Code)
		})
	}
}

// failingStore is a store whose session lookups fail with a database error.
type failingStore struct {
	store.Store
}

func (failingStore) Sessions() store.SessionsRepository {
	return failingSessions{}
}

type failingSessions struct {
	store.SessionsRepository
}

func (failingSessions) Get(context.Context, []byte) (store.Session, store.User, error) {
	return store.Session{}, store.User{}, errors.New("disk I/O error")
}

// failingStoreHandler returns the handler of a server on failingStore, with the web app.
func failingStoreHandler(t *testing.T) http.Handler {
	t.Helper()

	return NewHandler(Deps{
		Logger: slog.New(slog.DiscardHandler),
		Clock:  clock.NewFake(authTestNow),
		Store:  failingStore{},
		SPA:    newTestSPA(t, testDist()),
	})
}

// Given a session lookup that fails with a database error, then a protected operation answers 500
// internal, not 401: the session may well be valid. The operations without authentication, such as
// GET /api/v1/meta and the OpenAPI document, answer as usual.
func TestAuth_LookupErrorFailsOnlyProtectedOperations(t *testing.T) {
	t.Parallel()

	h := failingStoreHandler(t)
	cookie := "verve_session=c2Vzc2lvbg"

	rec := getWith(t, h, "/api/v1/auth/me", cookie)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.JSONEq(t, `{"type":"about:blank","title":"Internal Server Error","status":500,`+
		`"detail":"unexpected error occurred","code":"internal"}`, rec.Body.String())

	for _, target := range []string{"/api/v1/meta", "/api/openapi.json"} {
		rec := getWith(t, h, target, cookie)
		require.Equal(t, http.StatusOK, rec.Code, target)
	}
}

// Given a session cookie outside /api/, then no session lookup runs: the web app and the health
// probes answer as usual, even when the lookup would fail.
func TestAuth_NoLookupOutsideTheAPI(t *testing.T) {
	t.Parallel()

	h := failingStoreHandler(t)

	for _, target := range []string{"/healthz", "/readyz", "/", "/workouts/123"} {
		rec := getWith(t, h, target, "verve_session=c2Vzc2lvbg")
		require.Equal(t, http.StatusOK, rec.Code, target)
	}
}

// Given a gate that answers in place of the routes, then it answers before the CSRF checks and the
// session lookup, which need a migrated database.
func TestAuth_GateAnswersBeforeCSRFAndAuthentication(t *testing.T) {
	t.Parallel()

	h := NewHandler(Deps{
		Logger: slog.New(slog.DiscardHandler),
		Clock:  clock.NewFake(authTestNow),
		Store:  failingStore{},
		Gate: func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				WriteProblem(w, Problem{Status: http.StatusServiceUnavailable, Code: "starting"})
			})
		},
	})
	cookie := "verve_session=c2Vzc2lvbg"

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/auth/me", nil)

		rec := serveWith(h, r, cookie)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code, method)
		require.JSONEq(t, `{"type":"about:blank","title":"Service Unavailable","status":503,"code":"starting"}`,
			rec.Body.String(), method)
	}
}

// Given a lookup that renews the session, then the response sends the session cookie again with the
// renewed expiry, so that the browser keeps it as long as the session lives (FR-7). A lookup that
// does not renew the session sends no cookie.
func TestAuth_RenewedSessionSendsTheCookieAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		publicURL  string
		cookieName string
		// attributes follow the identifier in the cookie sent again.
		attributes string
	}{
		{
			publicURL:  "",
			cookieName: "verve_session",
			attributes: "; Path=/; Expires=Fri, 04 Dec 2026 08:01:00 GMT; HttpOnly; SameSite=Lax",
		},
		{
			publicURL:  "https://verve.example.com",
			cookieName: "__Host-verve_session",
			attributes: "; Path=/; Expires=Fri, 04 Dec 2026 08:01:00 GMT; HttpOnly; Secure; SameSite=Lax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cookieName, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			id := a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))
			h := a.handler(t, tt.publicURL)

			a.clock.Advance(time.Minute)

			rec := getWith(t, h, "/api/v1/auth/me", tt.cookieName+"="+id)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, []string{tt.cookieName + "=" + id + tt.attributes}, rec.Header().Values("Set-Cookie"))

			a.clock.Advance(30 * time.Second)

			rec = getWith(t, h, "/api/v1/auth/me", tt.cookieName+"="+id)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Empty(t, rec.Header().Values("Set-Cookie"))
		})
	}
}
