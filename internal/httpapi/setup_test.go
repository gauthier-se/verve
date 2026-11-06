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

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// setupPath is the path of the setup operations.
const setupPath = "/api/v1/setup"

// setupHandler returns the handler of a server on the store of a, without a public URL, whose
// setup token is token.
func (a authTest) setupHandler(t *testing.T, token *auth.SetupToken) http.Handler {
	t.Helper()

	deps := a.deps(t, "")
	deps.SetupToken = token

	return NewHandler(deps)
}

// setupBody returns the body of a setup with token, username and password.
func setupBody(t *testing.T, token, username, password string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{"token": token, "username": username, "password": password})
	require.NoError(t, err)

	return string(body)
}

// postSetup posts body as a setup on h, an instance without a public URL, from the phone, and
// returns the response.
func postSetup(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, sameOriginPost(t, "http://example.com", setupPath, body, phoneAddr))
}

// Given the right token and a valid account, then POST /api/v1/setup creates an admin, opens a
// session and answers 201 with the user and the session cookie. The admin then logs in with the
// password.
func TestSetup_RightTokenCreatesTheAdminAndOpensASession(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)

	var token auth.SetupToken

	h := a.setupHandler(t, &token)

	rec := postSetup(t, h, setupBody(t, token.Issue(), "Alice", alicePassword))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"id":1,"username":"Alice","role":"admin","timezone":null}`, rec.Body.String())

	id := sessionCookieOf(t, rec, "verve_session")
	require.Equal(t, []string{"verve_session=" + id + "; Path=/; Expires=Fri, 04 Dec 2026 08:00:00 GMT; HttpOnly; SameSite=Lax"},
		rec.Header().Values("Set-Cookie"))

	me := getWith(t, h, "/api/v1/auth/me", "verve_session="+id)
	require.Equal(t, http.StatusOK, me.Code)
	require.JSONEq(t, `{"id":1,"username":"Alice","role":"admin","timezone":null}`, me.Body.String())

	login := loginAt(t, h, laptopAddr, "alice", alicePassword)
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
}

// Given a setup that creates the admin, then it records the creation with the client of the request
// and the admin as subject. A rejected setup records nothing.
func TestSetup_RecordsTheCreationOfTheAdmin(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)

	var token auth.SetupToken

	issued := token.Issue()
	h := a.setupHandler(t, &token)

	requireProblem(t, postSetup(t, h, setupBody(t, "bm90LXRoZS1zZXR1cC10b2tlbg", "alice", alicePassword)),
		http.StatusForbidden, "setup_token_invalid")

	rec := postSetup(t, h, setupBody(t, issued, "alice", alicePassword))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	requireProblem(t, postSetup(t, h, setupBody(t, issued, "bob", alicePassword)), http.StatusConflict, "setup_completed")

	require.Equal(t, []recordedEvent{
		{Type: "user.created", SubjectID: 1, ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{"role":"admin","source":"setup"}`},
	}, a.recordedEvents(t))
}

// requireProblem checks that rec is the problem of status with code, without cookie.
func requireProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	require.Equal(t, status, rec.Code, rec.Body.String())
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, fmt.Sprintf(`{"type":"about:blank","title":%q,"status":%d,"code":%q}`,
		http.StatusText(status), status, code), rec.Body.String())
	require.Empty(t, rec.Header().Values("Set-Cookie"))
}

// Given the right token and a username or password that breaks the rules, then POST /api/v1/setup
// answers 422 with the code of the broken rule and creates no user. The token stays valid.
func TestSetup_InvalidAccountAnswers422WithTheCodeOfTheRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
		code     string
	}{
		{name: "empty username", username: "", password: alicePassword, code: "username_invalid"},
		{name: "username with an @", username: "alice@example.com", password: alicePassword, code: "username_invalid"},
		{name: "username of 65 characters", username: strings.Repeat("é", 65), password: alicePassword, code: "username_invalid"},
		{name: "password of 11 characters", username: "alice", password: "abcdefghijk", code: "password_too_short"},
		{name: "password of 129 characters", username: "alice", password: strings.Repeat("é", 129), code: "password_too_long"},
		{name: "common password", username: "alice", password: "Password1234", code: "password_too_common"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)

			var token auth.SetupToken

			issued := token.Issue()
			h := a.setupHandler(t, &token)

			rec := postSetup(t, h, setupBody(t, issued, tt.username, tt.password))

			requireProblem(t, rec, http.StatusUnprocessableEntity, tt.code)
			a.requireUserCount(t, 0)

			rec = postSetup(t, h, setupBody(t, issued, "alice", alicePassword))
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		})
	}
}

// requireUserCount checks that the store of a holds want users.
func (a authTest) requireUserCount(t *testing.T, want int64) {
	t.Helper()

	count, err := a.store.Users().Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, want, count)
}

// Given a wrong token, including a prefix or an extension of the right one, an empty token, or no
// token issued at all, then POST /api/v1/setup answers 403 setup_token_invalid and creates no user.
func TestSetup_WrongTokenAnswers403(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// token returns the token to send, from the issued one.
		token  func(issued string) string
		issued bool
	}{
		{name: "wrong", token: func(string) string { return "bm90LXRoZS1zZXR1cC10b2tlbg" }, issued: true},
		{name: "prefix of the right one", token: func(s string) string { return s[:len(s)-1] }, issued: true},
		{name: "extension of the right one", token: func(s string) string { return s + "A" }, issued: true},
		{name: "empty", token: func(string) string { return "" }, issued: true},
		{name: "none issued", token: func(string) string { return "bm90LXRoZS1zZXR1cC10b2tlbg" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)

			var token auth.SetupToken

			issued := ""
			if tt.issued {
				issued = token.Issue()
			}

			rec := postSetup(t, a.setupHandler(t, &token), setupBody(t, tt.token(issued), "alice", alicePassword))

			requireProblem(t, rec, http.StatusForbidden, "setup_token_invalid")
			a.requireUserCount(t, 0)
		})
	}
}

// Given a user created after the server started, as by verve user create next to it, then POST
// /api/v1/setup answers 409 setup_completed, with the right token as with a wrong one, and creates no
// user.
func TestSetup_UserExistsAnswers409WhateverTheToken(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)

	var token auth.SetupToken

	issued := token.Issue()
	h := a.setupHandler(t, &token)

	a.mustCreateUser(t, "bob", store.RoleMember)

	for _, sent := range []string{issued, "bm90LXRoZS1zZXR1cC10b2tlbg"} {
		rec := postSetup(t, h, setupBody(t, sent, "alice", alicePassword))

		requireProblem(t, rec, http.StatusConflict, "setup_completed")
		a.requireUserCount(t, 1)
	}
}

// deleteEveryUser deletes the users of the database of a, and their sessions, as a deletion of
// accounts would. No repository deletes a user yet.
func (a authTest) deleteEveryUser(t *testing.T) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), a.dbPath)
	require.NoError(t, err)

	defer func() { require.NoError(t, db.CloseShared()) }()

	_, err = db.Write().ExecContext(t.Context(), "DELETE FROM users")
	require.NoError(t, err)
}

// Given a setup that found a user, because it created the admin or because one already existed, then
// the token stops being valid: once no user is left, it answers 403 setup_token_invalid and creates
// no user, so that only a restart issues a token again.
func TestSetup_TokenStopsBeingValidOnceAUserExisted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// firstUser makes the first user exist, with the issued token at hand.
		firstUser func(t *testing.T, a authTest, h http.Handler, issued string)
	}{
		{
			name: "created by the setup",
			firstUser: func(t *testing.T, _ authTest, h http.Handler, issued string) {
				rec := postSetup(t, h, setupBody(t, issued, "alice", alicePassword))
				require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			},
		},
		{
			name: "created by the command line, then seen by a setup",
			firstUser: func(t *testing.T, a authTest, h http.Handler, issued string) {
				a.mustCreateUser(t, "bob", store.RoleMember)
				requireProblem(t, postSetup(t, h, setupBody(t, issued, "alice", alicePassword)),
					http.StatusConflict, "setup_completed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)

			var token auth.SetupToken

			issued := token.Issue()
			h := a.setupHandler(t, &token)

			tt.firstUser(t, a, h, issued)
			a.deleteEveryUser(t)

			requireProblem(t, postSetup(t, h, setupBody(t, issued, "carol", alicePassword)),
				http.StatusForbidden, "setup_token_invalid")
			a.requireUserCount(t, 0)
		})
	}
}

// countBarrierStore wraps a real store, so that a count of the users read outside a transaction
// signals arrived once read, then waits for release: the setups that read it all see no user before
// any of them writes.
type countBarrierStore struct {
	store.Store

	arrived chan<- struct{}
	release <-chan struct{}
}

func (s countBarrierStore) Users() store.UsersRepository {
	return countBarrierUsers{UsersRepository: s.Store.Users(), arrived: s.arrived, release: s.release}
}

type countBarrierUsers struct {
	store.UsersRepository

	arrived chan<- struct{}
	release <-chan struct{}
}

func (u countBarrierUsers) Count(ctx context.Context) (int64, error) {
	count, err := u.UsersRepository.Count(ctx)
	u.arrived <- struct{}{}
	<-u.release

	return count, err
}

// Given two concurrent setups with the right token, which both read that no user exists, then only
// one creates a user: the other answers 409 setup_completed.
func TestSetup_ConcurrentSetupsCreateOneUser(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)

	var token auth.SetupToken

	issued := token.Issue()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})

	deps := a.deps(t, "")
	deps.SetupToken = &token
	deps.Store = countBarrierStore{Store: a.store, arrived: arrived, release: release}
	h := NewHandler(deps)

	// Different usernames, so that the unique username cannot be what keeps the second user out.
	requests := []*http.Request{
		sameOriginPost(t, "http://example.com", setupPath, setupBody(t, issued, "alice", alicePassword), phoneAddr),
		sameOriginPost(t, "http://example.com", setupPath, setupBody(t, issued, "bob", alicePassword), laptopAddr),
	}

	statuses := make(chan int, len(requests))
	for _, r := range requests {
		go func() { statuses <- serveWith(h, r).Code }()
	}

	for range requests {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("both setups must read the count of the users")
		}
	}

	close(release)

	got := []int{<-statuses, <-statuses}
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, got)
	a.requireUserCount(t, 1)
}

// Given no user, then GET /api/v1/setup answers {"required": true} without authentication. Given a
// user, then it answers {"required": false}, read at each request.
func TestSetup_StatusTellsWhetherSetupIsRequired(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")

	rec := getWith(t, h, setupPath)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"required":true}`, rec.Body.String())

	a.mustCreateUser(t, "alice", store.RoleMember)

	rec = getWith(t, h, setupPath)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"required":false}`, rec.Body.String())
}
