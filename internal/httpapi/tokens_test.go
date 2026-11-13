package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
)

// tokensPath is the path of the API tokens of the current user.
const tokensPath = "/api/v1/account/tokens" //nolint:gosec // G101: a path, not a credential

// tokenPath returns the path of the token of id.
func tokenPath(id int64) string { return tokensPath + "/" + strconv.FormatInt(id, 10) }

// createdToken is the response of a token creation, as the tests read it.
type createdToken struct {
	ID     int64  `json:"id"`
	Token  string `json:"token"`
	Prefix string `json:"prefix"`
}

// createToken creates a token on h with body, with the cookies, and returns the response.
func createToken(t *testing.T, h http.Handler, body string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	return accountRequest(t, h, http.MethodPost, tokensPath, body, cookies...)
}

// mustCreateTokenThroughAPI creates a token on h with body, with the cookies, requires 201 and
// returns the response.
func mustCreateTokenThroughAPI(t *testing.T, h http.Handler, body string, cookies ...string) createdToken {
	t.Helper()

	rec := createToken(t, h, body, cookies...)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var created createdToken
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	return created
}

// bearerRequest sends method target on h with the token in Authorization: Bearer and the JSON body,
// or none when it is empty, without cookie nor CSRF header, and returns the response.
func bearerRequest(t *testing.T, h http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	r := httptest.NewRequestWithContext(t.Context(), method, target, reader)
	r.Header.Set("Authorization", "Bearer "+token)

	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	return serveWith(h, r)
}

// Given a session, then POST /api/v1/account/tokens with a name, scopes and an optional expiry
// answers 201 with the record of the token and the full token, once: vrv_ and 43 base62
// characters, whose first 8 characters are the prefix. The token authenticates, and the list of the
// tokens never shows it again.
func TestCreateToken_AnswersTheFullTokenOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		// want is the response without the id, the prefix and the token.
		want string
	}{
		{
			name: "with expiry",
			body: `{"name":"Health Auto Export","scopes":["ingest","read"],"expiresAt":"2026-12-04T08:00:00Z"}`,
			want: `{"name":"Health Auto Export","scopes":["ingest","read"],"createdAt":"2026-11-04T08:00:00Z",
				"expiresAt":"2026-12-04T08:00:00Z"}`,
		},
		{
			name: "without expiry",
			body: `{"name":"Home Assistant","scopes":["read"]}`,
			want: `{"name":"Home Assistant","scopes":["read"],"createdAt":"2026-11-04T08:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAccountTest(t)

			rec := createToken(t, a.handler, tt.body, a.aliceCookie)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

			token, ok := body["token"].(string)
			require.True(t, ok, rec.Body.String())
			require.True(t, auth.LooksLikeAPIToken(token), token)
			require.Equal(t, token[:8], body["prefix"])
			require.NotZero(t, body["id"])

			delete(body, "token")
			delete(body, "prefix")
			delete(body, "id")

			rest, err := json.Marshal(body)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(rest))

			var principal auth.Principal

			used := getWithAuthorization(t, scopedAPIHandler(t, a.authTest, &principal), "/api/v1/test/read", "Bearer "+token)
			require.Equal(t, http.StatusNoContent, used.Code, used.Body.String())
			require.Equal(t, a.alice.ID, principal.UserID)

			list := accountRequest(t, a.handler, http.MethodGet, tokensPath, "", a.aliceCookie)
			require.Equal(t, http.StatusOK, list.Code, list.Body.String())
			require.NotContains(t, list.Body.String(), token)
		})
	}
}

// Given tokens of the user, used, revoked or never used, and a token of another user, then GET
// /api/v1/account/tokens lists the tokens of the user only, newest first, with their name, scopes,
// prefix, dates and last used IP, and never the token or its hash.
func TestListTokens_ListsTheTokensOfTheUserWithoutSecrets(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)

	sync := mustCreateTokenThroughAPI(t, a.handler,
		`{"name":"sync","scopes":["ingest"],"expiresAt":"2027-11-04T08:00:00Z"}`, a.aliceCookie)
	mustCreateTokenThroughAPI(t, a.handler, `{"name":"bob","scopes":["read"]}`, a.bobCookie)

	a.clock.Advance(time.Hour)
	sensor := mustCreateTokenThroughAPI(t, a.handler, `{"name":"sensor","scopes":["read","ingest"]}`, a.aliceCookie)

	a.clock.Advance(time.Hour)

	var principal auth.Principal

	used := getWithAuthorization(t, scopedAPIHandler(t, a.authTest, &principal), "/api/v1/test/ingest", "Bearer "+sync.Token)
	require.Equal(t, http.StatusNoContent, used.Code, used.Body.String())

	a.clock.Advance(time.Hour)
	require.Equal(t, http.StatusNoContent,
		accountRequest(t, a.handler, http.MethodDelete, tokenPath(sensor.ID), "", a.aliceCookie).Code)

	rec := accountRequest(t, a.handler, http.MethodGet, tokensPath, "", a.aliceCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"items":[
		{"id":%d,"name":"sensor","scopes":["read","ingest"],"prefix":%q,"createdAt":"2026-11-04T09:00:00Z",
		 "revokedAt":"2026-11-04T11:00:00Z"},
		{"id":%d,"name":"sync","scopes":["ingest"],"prefix":%q,"createdAt":"2026-11-04T08:00:00Z",
		 "expiresAt":"2027-11-04T08:00:00Z","lastUsedAt":"2026-11-04T10:00:00Z","lastUsedIp":"192.0.2.1"}
	]}`, sensor.ID, sensor.Prefix, sync.ID, sync.Prefix), rec.Body.String())
}

// Given a user without tokens, then GET /api/v1/account/tokens answers an empty list.
func TestListTokens_UserWithoutTokensGetsAnEmptyList(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)

	rec := accountRequest(t, a.handler, http.MethodGet, tokensPath, "", a.aliceCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"items":[]}`, rec.Body.String())
}

// Given DELETE /api/v1/account/tokens/{id}, then it answers 204 and the next request with the token
// answers 401 token_revoked. A second revocation answers 204 too. Given the token of another user or
// an unknown id, then 404, and the token of the other user still authenticates.
func TestRevokeToken_RevokesATokenOfTheUserOnly(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	aliceToken := mustCreateTokenThroughAPI(t, a.handler, `{"name":"sync","scopes":["read"]}`, a.aliceCookie)
	bobToken := mustCreateTokenThroughAPI(t, a.handler, `{"name":"sync","scopes":["read"]}`, a.bobCookie)

	require.Equal(t, http.StatusOK, bearerRequest(t, a.handler, http.MethodGet, "/api/v1/meta", "", aliceToken.Token).Code)

	rec := accountRequest(t, a.handler, http.MethodDelete, tokenPath(aliceToken.ID), "", a.aliceCookie)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	status, code := problemCode(t, bearerRequest(t, a.handler, http.MethodGet, "/api/v1/meta", "", aliceToken.Token))
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "token_revoked", code)

	rec = accountRequest(t, a.handler, http.MethodDelete, tokenPath(aliceToken.ID), "", a.aliceCookie)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	for _, id := range []int64{bobToken.ID, bobToken.ID + 100} {
		status, code := problemCode(t, accountRequest(t, a.handler, http.MethodDelete, tokenPath(id), "", a.aliceCookie))
		require.Equal(t, http.StatusNotFound, status)
		require.Equal(t, "not_found", code)
	}

	require.Equal(t, http.StatusOK, bearerRequest(t, a.handler, http.MethodGet, "/api/v1/meta", "", bobToken.Token).Code)
}

// Given an unknown, missing or repeated scope, an empty or too long name, or an expiry that is not
// in the future, then POST /api/v1/account/tokens answers 422 and creates no token.
func TestCreateToken_InvalidScopesAndPastExpiryAre422(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "unknown scope", body: `{"name":"sync","scopes":["admin"]}`, code: "validation_failed"},
		{name: "unknown and known scope", body: `{"name":"sync","scopes":["read","write"]}`, code: "validation_failed"},
		{name: "no scope", body: `{"name":"sync","scopes":[]}`, code: "validation_failed"},
		{name: "missing scopes", body: `{"name":"sync"}`, code: "validation_failed"},
		{name: "repeated scope", body: `{"name":"sync","scopes":["read","read"]}`, code: "validation_failed"},
		{name: "empty name", body: `{"name":"","scopes":["read"]}`, code: "validation_failed"},
		{name: "name of 65 characters", body: fmt.Sprintf(`{"name":%q,"scopes":["read"]}`, strings.Repeat("a", 65)), code: "validation_failed"},
		{name: "expiry in the past", body: `{"name":"sync","scopes":["read"],"expiresAt":"2026-11-04T07:59:59Z"}`, code: "expiry_in_past"},
		{name: "expiry now", body: `{"name":"sync","scopes":["read"],"expiresAt":"2026-11-04T08:00:00Z"}`, code: "expiry_in_past"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAccountTest(t)

			status, code := problemCode(t, createToken(t, a.handler, tt.body, a.aliceCookie))
			require.Equal(t, http.StatusUnprocessableEntity, status)
			require.Equal(t, tt.code, code)

			tokens, err := a.store.APITokens().ListByUser(t.Context(), a.alice.ID)
			require.NoError(t, err)
			require.Empty(t, tokens)
			require.Empty(t, a.recordedEvents(t))
		})
	}
}

// Given a request authenticated by a token, even with every scope and a valid body, then the token
// operations answer 401 unauthorized: a token cannot create, list or revoke tokens. Given no
// authentication, then 401 too.
func TestTokens_RequireASession(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	created := mustCreateTokenThroughAPI(t, a.handler, `{"name":"sync","scopes":["ingest","read"]}`, a.aliceCookie)

	for _, op := range []struct{ method, target, body string }{
		{http.MethodPost, tokensPath, `{"name":"sync","scopes":["read"]}`},
		{http.MethodGet, tokensPath, ""},
		{http.MethodDelete, tokenPath(created.ID), ""},
	} {
		status, code := problemCode(t, bearerRequest(t, a.handler, op.method, op.target, op.body, created.Token))
		require.Equal(t, http.StatusUnauthorized, status, op)
		require.Equal(t, "unauthorized", code, op)

		status, code = problemCode(t, accountRequest(t, a.handler, op.method, op.target, op.body))
		require.Equal(t, http.StatusUnauthorized, status, op)
		require.Equal(t, "unauthorized", code, op)
	}

	tokens, err := a.store.APITokens().ListByUser(t.Context(), a.alice.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.True(t, tokens[0].RevokedAt.IsZero())
}

// Given a token above its rate limit, then its request answers 429 rate_limited with Retry-After
// in seconds, before reaching the operation, and the other tokens are not limited.
func TestTokens_TokenAboveItsRateLimitAnswers429(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	limited := mustCreateTokenThroughAPI(t, a.handler, `{"name":"sync","scopes":["ingest"]}`, a.aliceCookie)
	other := mustCreateTokenThroughAPI(t, a.handler, `{"name":"other","scopes":["ingest"]}`, a.aliceCookie)

	var principal auth.Principal

	h := scopedAPIHandler(t, a.authTest, &principal)

	for i := range 120 {
		rec := getWithAuthorization(t, h, "/api/v1/test/ingest", "Bearer "+limited.Token)
		require.Equal(t, http.StatusNoContent, rec.Code, "request %d: %s", i+1, rec.Body.String())
	}

	principal = auth.Principal{}
	rec := getWithAuthorization(t, h, "/api/v1/test/ingest", "Bearer "+limited.Token)

	status, code := problemCode(t, rec)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "rate_limited", code)
	require.Equal(t, []string{"1"}, rec.Header().Values("Retry-After"))
	require.Equal(t, auth.Principal{}, principal)

	rec = getWithAuthorization(t, h, "/api/v1/test/ingest", "Bearer "+other.Token)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	a.clock.Advance(time.Second)
	rec = getWithAuthorization(t, h, "/api/v1/test/ingest", "Bearer "+limited.Token)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// Given an admin, then the admin user list shows the number of active tokens of each user, neither
// revoked nor expired, and not the tokens.
func TestAdminUsers_ListShowsTheNumberOfTokensPerUser(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)
	h := u.handler
	bobCookie := u.bobCookie

	mustCreateTokenThroughAPI(t, h, `{"name":"sync","scopes":["ingest"]}`, bobCookie)
	mustCreateTokenThroughAPI(t, h, `{"name":"expiring","scopes":["read"],"expiresAt":"2026-11-04T09:00:00Z"}`, bobCookie)
	revoked := mustCreateTokenThroughAPI(t, h, `{"name":"revoked","scopes":["read"]}`, bobCookie)
	mustCreateTokenThroughAPI(t, h, `{"name":"sensor","scopes":["read"]}`, bobCookie)
	require.Equal(t, http.StatusNoContent, accountRequest(t, h, http.MethodDelete, tokenPath(revoked.ID), "", bobCookie).Code)

	u.clock.Advance(2 * time.Hour)

	rec := getWith(t, h, adminUsersPath, u.adminCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"items":[
		{"id":%d,"username":"bob","email":"bob@example.com","role":"member","status":"active",
		 "createdAt":"2026-11-04T08:00:00Z","activeTokens":2,"samples":0,"storageBytes":0,"lastIngestions":{}},
		{"id":%d,"username":"admin","email":"admin@example.com","role":"admin","status":"active",
		 "createdAt":"2026-11-04T08:00:00Z","activeTokens":0,"samples":0,"storageBytes":0,"lastIngestions":{}}
	]}`, u.bob.ID, u.admin.ID), rec.Body.String())
}

// Given a creation and a revocation, then each records its audit event with the user as actor, the
// id and the name of the token, and the client of the request: the creation with the scopes too. A
// second revocation, a revocation of the token of another user and a rejected creation record
// nothing.
func TestTokens_RecordAuditEvents(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	created := mustCreateTokenThroughAPI(t, a.handler, `{"name":"sync","scopes":["ingest","read"]}`, a.aliceCookie)
	bob := mustCreateTokenThroughAPI(t, a.handler, `{"name":"bob","scopes":["read"]}`, a.bobCookie)

	require.Equal(t, http.StatusUnprocessableEntity, createToken(t, a.handler, `{"name":"x","scopes":["admin"]}`, a.aliceCookie).Code)

	for range 2 {
		require.Equal(t, http.StatusNoContent,
			accountRequest(t, a.handler, http.MethodDelete, tokenPath(created.ID), "", a.aliceCookie).Code)
	}

	require.Equal(t, http.StatusNotFound,
		accountRequest(t, a.handler, http.MethodDelete, tokenPath(bob.ID), "", a.aliceCookie).Code)

	require.Equal(t, []recordedEvent{
		{
			Type: "token.created", ActorID: a.alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent,
			Payload: fmt.Sprintf(`{"id":%d,"name":"sync","scopes":["ingest","read"]}`, created.ID),
		},
		{
			Type: "token.created", ActorID: a.bob.ID, ClientIP: "203.0.113.7", UserAgent: userAgent,
			Payload: fmt.Sprintf(`{"id":%d,"name":"bob","scopes":["read"]}`, bob.ID),
		},
		{
			Type: "token.revoked", ActorID: a.alice.ID, ClientIP: "203.0.113.7", UserAgent: userAgent,
			Payload: fmt.Sprintf(`{"id":%d,"name":"sync"}`, created.ID),
		},
	}, a.recordedEvents(t))
}

// Given a user with tokens, then the status change, the role change and the deletion of the user by
// an admin answer the user as the list shows them, with the number of their active tokens, which
// none of these changes revokes.
func TestAdminUsers_ChangesAnswerTheNumberOfTokensAsTheListDoes(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	mustCreateTokenThroughAPI(t, u.handler, `{"name":"sync","scopes":["ingest"]}`, u.bobCookie)
	mustCreateTokenThroughAPI(t, u.handler, `{"name":"sensor","scopes":["read"]}`, u.bobCookie)

	for _, change := range []struct {
		method, target, body string
		status               int
	}{
		{http.MethodPut, userRolePath(u.bob.ID), `{"role":"admin"}`, http.StatusOK},
		{http.MethodPut, userStatusPath(u.bob.ID), `{"status":"disabled"}`, http.StatusOK},
		{http.MethodDelete, adminUserPath(u.bob.ID), `{"confirmation":"bob"}`, http.StatusAccepted},
	} {
		rec := accountRequest(t, u.handler, change.method, change.target, change.body, u.adminCookie)

		require.Equal(t, change.status, rec.Code, rec.Body.String())
		require.JSONEq(t, `2`, mustField(t, rec.Body.Bytes(), "activeTokens"), change.target)
	}
}
