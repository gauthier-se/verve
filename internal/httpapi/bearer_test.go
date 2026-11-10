package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
)

// mustCreateToken creates a token of user with scopes, which expires at expiresAt, or never when it
// is zero, and returns the token and its record.
func (a authTest) mustCreateToken(t *testing.T, user store.User, expiresAt time.Time, scopes ...store.TokenScope) (string, store.APIToken) {
	t.Helper()

	token, created, err := auth.CreateAPIToken(t.Context(), a.store, auth.NewAPIToken{
		UserID:    user.ID,
		Name:      "sync",
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	})
	require.NoError(t, err)

	return token, created
}

// scopedAPIHandler returns the handler of a server on the store of a, with the test-only operations
// GET /api/v1/test/ingest and GET /api/v1/test/read, which require the scope of their name and
// answer 204. The principal of the last request that reached one of them is stored in principal.
func scopedAPIHandler(t *testing.T, a authTest, principal *auth.Principal) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	api := NewAPI(mux, APIDeps{Store: a.store})

	for _, scope := range []store.TokenScope{store.ScopeIngest, store.ScopeRead} {
		huma.Register(api, huma.Operation{
			OperationID: "test-" + string(scope),
			Method:      http.MethodGet,
			Path:        "/api/v1/test/" + string(scope),
		}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			got, err := requireScope(ctx, scope)
			if err != nil {
				return nil, err
			}

			*principal = got

			return nil, nil
		})
	}

	return newHandler(a.deps(t, ""), mux)
}

// getWithAuthorization serves GET target on h with authorization as the Authorization header, and
// returns the response.
func getWithAuthorization(t *testing.T, h http.Handler, target, authorization string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	r.Header.Set("Authorization", authorization)

	return serveWith(h, r)
}

// problemCode returns the status and the code of the problem that rec holds.
func problemCode(t *testing.T, rec *httptest.ResponseRecorder) (int, string) {
	t.Helper()

	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"), rec.Body.String())

	var p Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	require.Equal(t, rec.Code, p.Status)

	return p.Status, p.Code
}

// Given a valid token on an operation that requires one of its scopes, then the operation runs with
// a principal holding the user, the token and its scopes, and the method token, whatever the case of
// the scheme, with surrounding spaces or with a doubled Bearer prefix.
func TestBearer_ValidTokenAuthenticatesTheRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// header returns the Authorization header for token.
		header func(token string) string
	}{
		{name: "Bearer", header: func(token string) string { return "Bearer " + token }},
		{name: "lowercase scheme", header: func(token string) string { return "bearer " + token }},
		{name: "surrounding spaces", header: func(token string) string { return "  Bearer   " + token + "  " }},
		{name: "doubled prefix", header: func(token string) string { return "Bearer Bearer " + token }},
		{name: "doubled prefix in another case", header: func(token string) string { return "Bearer bearer  " + token }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			user := a.mustCreateUser(t, "alice", store.RoleMember)
			token, created := a.mustCreateToken(t, user, time.Time{}, store.ScopeIngest, store.ScopeRead)

			var got auth.Principal

			rec := getWithAuthorization(t, scopedAPIHandler(t, a, &got), "/api/v1/test/ingest", tt.header(token))

			require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
			require.Equal(t, auth.Principal{
				UserID:  user.ID,
				Method:  auth.MethodToken,
				TokenID: created.ID,
				Scopes:  []store.TokenScope{store.ScopeIngest, store.ScopeRead},
			}, got)
		})
	}
}

// Given a session on an operation that requires a scope, then it runs: the SPA reads with its
// session what a read token reads.
func TestBearer_ScopedOperationAcceptsASession(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	user := a.mustCreateUser(t, "alice", store.RoleMember)

	var got auth.Principal

	rec := getWith(t, scopedAPIHandler(t, a, &got), "/api/v1/test/read", "verve_session="+a.mustCreateSession(t, user))

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, auth.Principal{UserID: user.ID, Role: store.RoleMember, Method: auth.MethodSession}, got)
}

// Given a token without the scope that an operation requires, then 403 scope_missing: an ingest
// token cannot read, and a read token cannot ingest.
func TestBearer_MissingScopeIsForbidden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope  store.TokenScope
		target string
	}{
		{scope: store.ScopeIngest, target: "/api/v1/test/read"},
		{scope: store.ScopeRead, target: "/api/v1/test/ingest"},
	}

	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			token, _ := a.mustCreateToken(t, a.mustCreateUser(t, "alice", store.RoleMember), time.Time{}, tt.scope)

			var got auth.Principal

			rec := getWithAuthorization(t, scopedAPIHandler(t, a, &got), tt.target, "Bearer "+token)

			status, code := problemCode(t, rec)
			require.Equal(t, http.StatusForbidden, status)
			require.Equal(t, "scope_missing", code)
			require.Equal(t, auth.Principal{}, got)
		})
	}
}

// Given a revoked token, an expired token, a token of a disabled user or an unknown token, then 401
// with token_revoked, token_expired, user_disabled or unauthorized, also on an operation that needs
// no authentication, since the client must learn that its credential no longer works.
func TestBearer_RejectedTokenIsUnauthorized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// token returns the token to send, of the user alice.
		token func(t *testing.T, a authTest, alice store.User) string
		want  string
	}{
		{
			name: "revoked",
			token: func(t *testing.T, a authTest, alice store.User) string {
				t.Helper()

				token, created := a.mustCreateToken(t, alice, time.Time{}, store.ScopeIngest)
				_, err := a.store.APITokens().Revoke(t.Context(), alice.ID, created.ID)
				require.NoError(t, err)

				return token
			},
			want: "token_revoked",
		},
		{
			name: "expired",
			token: func(t *testing.T, a authTest, alice store.User) string {
				t.Helper()

				token, _ := a.mustCreateToken(t, alice, authTestNow.Add(time.Hour), store.ScopeIngest)
				a.clock.Advance(2 * time.Hour)

				return token
			},
			want: "token_expired",
		},
		{
			name: "disabled user",
			token: func(t *testing.T, a authTest, alice store.User) string {
				t.Helper()

				token, _ := a.mustCreateToken(t, alice, time.Time{}, store.ScopeIngest)
				_, err := a.store.Users().SetStatus(t.Context(), alice.ID, store.UserDisabled)
				require.NoError(t, err)

				return token
			},
			want: "user_disabled",
		},
		{
			name: "unknown",
			token: func(*testing.T, authTest, store.User) string {
				return unknownToken
			},
			want: "unauthorized",
		},
	}

	for _, tt := range tests {
		for _, target := range []string{"/api/v1/test/ingest", "/api/v1/meta"} {
			t.Run(tt.name+" on "+target, func(t *testing.T) {
				t.Parallel()

				a := newAuthTest(t)
				token := tt.token(t, a, a.mustCreateUser(t, "alice", store.RoleMember))

				var got auth.Principal

				rec := getWithAuthorization(t, scopedAPIHandler(t, a, &got), target, "Bearer "+token)

				status, code := problemCode(t, rec)
				require.Equal(t, http.StatusUnauthorized, status)
				require.Equal(t, tt.want, code)
				require.Equal(t, auth.Principal{}, got)
			})
		}
	}
}

// unknownToken has the shape of an API token, but no token has it.
const unknownToken = "vrv_0000000000000000000000000000000000000000000"

// Given a token in the query string, then 400 token_in_query, with or without an Authorization
// header, on any path under /api/ and for any method, before the CSRF checks: the token is never
// used, so its last use stays unwritten.
func TestBearer_TokenInQueryIsRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// query returns the query string for token.
		query func(token string) string
		// authorized sends the token in the Authorization header too.
		authorized bool
		// method is the method of the request, GET when empty.
		method string
	}{
		{name: "access_token", query: func(token string) string { return "access_token=" + token }},
		{name: "access_token in another case", query: func(token string) string { return "Access_Token=" + token }},
		{name: "any parameter", query: func(token string) string { return "page=2&token=" + token }},
		{name: "as a parameter name", query: func(token string) string { return token }},
		{name: "unknown token", query: func(string) string { return "key=" + unknownToken }},
		{name: "empty access_token", query: func(string) string { return "access_token=" }},
		{name: "with the header too", query: func(token string) string { return "access_token=" + token }, authorized: true},
		{name: "POST without the CSRF header", query: func(token string) string { return "access_token=" + token }, method: http.MethodPost},
	}

	for _, tt := range tests {
		for _, path := range []string{"/api/v1/test/ingest", "/api/v1/meta"} {
			t.Run(tt.name+" on "+path, func(t *testing.T) {
				t.Parallel()

				a := newAuthTest(t)
				alice := a.mustCreateUser(t, "alice", store.RoleMember)
				token, _ := a.mustCreateToken(t, alice, time.Time{}, store.ScopeIngest)

				var got auth.Principal

				r := httptest.NewRequestWithContext(t.Context(), cmp.Or(tt.method, http.MethodGet), path+"?"+tt.query(token), nil)
				if tt.authorized {
					r.Header.Set("Authorization", "Bearer "+token)
				}

				rec := serveWith(scopedAPIHandler(t, a, &got), r)

				status, code := problemCode(t, rec)
				require.Equal(t, http.StatusBadRequest, status)
				require.Equal(t, "token_in_query", code)
				require.Equal(t, auth.Principal{}, got)

				list, err := a.store.APITokens().ListByUser(t.Context(), alice.ID)
				require.NoError(t, err)
				require.True(t, list[0].LastUsedAt.IsZero())
			})
		}
	}
}

// Given query parameters that hold no token, such as a search for vrv_ or a value shorter than a
// token, then the request goes on.
func TestBearer_QueryWithoutTokenGoesOn(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	token, _ := a.mustCreateToken(t, a.mustCreateUser(t, "alice", store.RoleMember), time.Time{}, store.ScopeIngest)

	var got auth.Principal

	h := scopedAPIHandler(t, a, &got)

	for _, query := range []string{
		"page=2&name=vrv",
		"q=vrv_",
		"q=vrv_short",
		"q=" + unknownToken + "0",
		"q=" + strings.Replace(unknownToken, "0", "-", 1),
	} {
		rec := getWithAuthorization(t, h, "/api/v1/test/ingest?"+query, "Bearer "+token)

		require.Equal(t, http.StatusNoContent, rec.Code, "%s: %s", query, rec.Body.String())
	}
}

// Given a valid token on an operation that accepts sessions only, such as GET /api/v1/auth/me or an
// admin operation, then 401 unauthorized, even for the token of an admin.
func TestBearer_SessionOnlyOperationRejectsATokenPrincipal(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	admin := a.mustCreateUser(t, "root", store.RoleAdmin)
	token, _ := a.mustCreateToken(t, admin, time.Time{}, store.ScopeIngest, store.ScopeRead)

	for _, h := range []struct {
		handler http.Handler
		target  string
	}{
		{handler: a.handler(t, ""), target: "/api/v1/auth/me"},
		{handler: adminAPIHandler(t, a), target: "/api/v1/test/admin"},
	} {
		rec := getWithAuthorization(t, h.handler, h.target, "Bearer "+token)

		require.Equal(t, http.StatusUnauthorized, rec.Code, h.target)
		require.JSONEq(t, unauthorizedBody, rec.Body.String(), h.target)
	}
}

// Given an endless stream of requests with a revoked token, then each answers 401 token_revoked,
// and their audit events are aggregated: from the 21st, the rejections add to the 20th row.
func TestBearer_RejectionsAreAggregatedInTheAuditLog(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	alice := a.mustCreateUser(t, "alice", store.RoleMember)
	token, created := a.mustCreateToken(t, alice, time.Time{}, store.ScopeIngest)

	_, err := a.store.APITokens().Revoke(t.Context(), alice.ID, created.ID)
	require.NoError(t, err)

	var got auth.Principal

	h := scopedAPIHandler(t, a, &got)

	for range 30 {
		rec := getWithAuthorization(t, h, "/api/v1/test/ingest", "Bearer "+token)

		_, code := problemCode(t, rec)
		require.Equal(t, "token_revoked", code)
	}

	events, err := a.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: "token.rejected"}, store.PageRequest{Limit: 100})
	require.NoError(t, err)
	require.Len(t, events, 20)
	require.Equal(t, int64(11), events[0].Count)

	var count int64
	for _, event := range events {
		require.Equal(t, alice.ID, event.SubjectID)
		count += event.Count
	}

	require.Equal(t, int64(30), count)
}

// failingAuditStore is a store whose audit events fail to append.
type failingAuditStore struct {
	store.Store
}

func (failingAuditStore) AuditEvents() store.AuditEventsRepository {
	return failingAuditEvents{}
}

type failingAuditEvents struct {
	store.AuditEventsRepository
}

func (failingAuditEvents) Append(context.Context, store.NewAuditEvent) (store.AuditEvent, error) {
	return store.AuditEvent{}, errors.New("disk I/O error")
}

// failingTokensStore is a store whose token lookups fail with a database error.
type failingTokensStore struct {
	store.Store
}

func (failingTokensStore) APITokens() store.APITokensRepository {
	return failingTokens{}
}

type failingTokens struct {
	store.APITokensRepository
}

func (failingTokens) GetByHash(context.Context, []byte) (store.APIToken, store.User, error) {
	return store.APIToken{}, store.User{}, errors.New("disk I/O error")
}

// Given a token that cannot be checked, because its lookup fails or because the audit event of its
// rejection cannot be recorded, then 500 internal on any operation, also on one that needs no
// authentication: the request never goes on as if the token were fine.
func TestBearer_UncheckableTokenFailsTheRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// wrap returns the store of the server around the store of the test.
		wrap func(s store.Store) store.Store
	}{
		{name: "lookup fails", wrap: func(s store.Store) store.Store { return failingTokensStore{s} }},
		{name: "audit of the rejection fails", wrap: func(s store.Store) store.Store { return failingAuditStore{s} }},
	}

	for _, tt := range tests {
		for _, target := range []string{"/api/v1/meta", "/api/v1/auth/me"} {
			t.Run(tt.name+" on "+target, func(t *testing.T) {
				t.Parallel()

				a := newAuthTest(t)
				alice := a.mustCreateUser(t, "alice", store.RoleMember)
				token, created := a.mustCreateToken(t, alice, time.Time{}, store.ScopeIngest)

				_, err := a.store.APITokens().Revoke(t.Context(), alice.ID, created.ID)
				require.NoError(t, err)

				deps := a.deps(t, "")
				deps.Store = tt.wrap(a.store)

				rec := getWithAuthorization(t, NewHandler(deps), target, "Bearer "+token)

				require.Equal(t, http.StatusInternalServerError, rec.Code)
				require.JSONEq(t, `{"type":"about:blank","title":"Internal Server Error","status":500,`+
					`"detail":"unexpected error occurred","code":"internal"}`,
					rec.Body.String())
			})
		}
	}
}
