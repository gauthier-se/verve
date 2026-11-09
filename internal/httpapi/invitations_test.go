package httpapi

import (
	"context"
	"crypto/sha256"
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

// The paths of the invitation operations.
const (
	invitationsPath       = "/api/v1/admin/invitations"
	inspectInvitationPath = "/api/v1/invitations/inspect"
	acceptInvitationPath  = "/api/v1/invitations/accept"
)

// bobPassword is the password that the invitees of the tests choose.
const bobPassword = "a long passphrase for bob" //nolint:gosec // G101: a fake password of the tests

// invitationExpiry is when the invitations created at authTestNow expire: 72 hours later.
var invitationExpiry = authTestNow.Add(72 * time.Hour)

// jsonBody returns fields as a JSON body.
func jsonBody(t *testing.T, fields map[string]string) string {
	t.Helper()

	body, err := json.Marshal(fields)
	require.NoError(t, err)

	return string(body)
}

// postAt posts body to target on h, an instance at origin, from the phone with the cookies, and
// returns the response.
func postAt(t *testing.T, h http.Handler, origin, target, body string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, sameOriginPost(t, origin, target, body, phoneAddr), cookies...)
}

// createdInvitation is the response of a created invitation.
type createdInvitation struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
	Link      string    `json:"link"`
}

// mustInvite creates on h, an instance without a public URL, the invitation of fields as the admin
// of the session cookie, and returns its token.
func mustInvite(t *testing.T, h http.Handler, cookie string, fields map[string]string) string {
	t.Helper()

	rec := postAt(t, h, "http://example.com", invitationsPath, jsonBody(t, fields), cookie)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var created createdInvitation
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	token, found := strings.CutPrefix(created.Link, "/invite#")
	require.True(t, found, created.Link)

	return token
}

// adminSession stores an admin and returns the session cookie of a session of theirs, as
// name=value, and the admin.
func (a authTest) adminSession(t *testing.T) (string, store.User) {
	t.Helper()

	admin := a.mustCreateUser(t, "admin", store.RoleAdmin)

	return "verve_session=" + a.mustCreateSession(t, admin), admin
}

// Given an admin, then POST /api/v1/admin/invitations with a username, an email and a role creates
// an invitation valid 72 hours and answers 201 with its link, once. Only the hash of the token is
// stored.
func TestInvitations_AdminCreatesAnInvitationValid72Hours(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie, admin := a.adminSession(t)

	rec := postAt(t, h, "http://example.com", invitationsPath,
		jsonBody(t, map[string]string{"username": "Bob", "email": "bob@example.com", "role": "admin"}), cookie)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var created createdInvitation
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	token, found := strings.CutPrefix(created.Link, "/invite#")
	require.True(t, found, created.Link)
	require.Len(t, token, 43, "256 random bits in unpadded base64url")
	require.JSONEq(t, fmt.Sprintf(
		`{"id":%d,"username":"Bob","email":"bob@example.com","role":"admin","expiresAt":"2026-11-07T08:00:00Z","link":%q}`,
		created.ID, created.Link), rec.Body.String())

	hash := sha256.Sum256([]byte(token))
	stored, err := a.store.Invitations().GetByTokenHash(t.Context(), hash[:])
	require.NoError(t, err)
	require.Equal(t, store.Invitation{
		ID:        created.ID,
		Kind:      store.InvitationInvite,
		Username:  "Bob",
		Email:     "bob@example.com",
		Role:      store.RoleAdmin,
		ExpiresAt: invitationExpiry,
		CreatedBy: admin.ID,
		CreatedAt: authTestNow,
	}, stored)

	_, err = a.store.Invitations().GetByTokenHash(t.Context(), []byte(token))
	require.ErrorIs(t, err, store.ErrNotFound, "the token itself is never stored")
}

// Given a public URL, then the link of an invitation is <VERVE_PUBLIC_URL>/invite#<token>. Given
// none, then it is the path /invite#<token>, to which the client prepends its origin.
func TestInvitations_LinkStartsWithThePublicURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		publicURL string
		origin    string
		cookie    string
		prefix    string
	}{
		{name: "public URL", publicURL: "https://verve.example.com", origin: "https://verve.example.com",
			cookie: "__Host-verve_session", prefix: "https://verve.example.com/invite#"},
		{name: "no public URL", origin: "http://example.com", cookie: "verve_session", prefix: "/invite#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			h := a.handler(t, tt.publicURL)
			admin := a.mustCreateUser(t, "admin", store.RoleAdmin)
			cookie := tt.cookie + "=" + a.mustCreateSession(t, admin)

			rec := postAt(t, h, tt.origin, invitationsPath,
				jsonBody(t, map[string]string{"username": "bob", "role": "member"}), cookie)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

			var created createdInvitation
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

			token, found := strings.CutPrefix(created.Link, tt.prefix)
			require.True(t, found, created.Link)
			require.Len(t, token, 43)
			require.Empty(t, created.Email, "the email is optional")
		})
	}
}

// Given a username or an email that a user already has, in any case, then POST
// /api/v1/admin/invitations answers 409 username_taken or email_taken. A username or an email that
// breaks the rules answers 422 username_invalid or email_invalid. None creates an invitation.
func TestInvitations_TakenOrInvalidLoginIsRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields map[string]string
		status int
		code   string
	}{
		{name: "username taken", fields: map[string]string{"username": "ALICE", "role": "member"},
			status: http.StatusConflict, code: "username_taken"},
		{name: "email taken", fields: map[string]string{"username": "bob", "email": "Alice@Example.com", "role": "member"},
			status: http.StatusConflict, code: "email_taken"},
		{name: "username with an @", fields: map[string]string{"username": "bob@example.com", "role": "member"},
			status: http.StatusUnprocessableEntity, code: "username_invalid"},
		{name: "empty username", fields: map[string]string{"username": "", "role": "member"},
			status: http.StatusUnprocessableEntity, code: "username_invalid"},
		{name: "email without an @", fields: map[string]string{"username": "bob", "email": "bob", "role": "member"},
			status: http.StatusUnprocessableEntity, code: "email_invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			h := a.handler(t, "")
			cookie, _ := a.adminSession(t)
			a.mustCreateUser(t, "alice", store.RoleMember)

			rec := postAt(t, h, "http://example.com", invitationsPath, jsonBody(t, tt.fields), cookie)

			requireProblem(t, rec, tt.status, tt.code)
			require.Equal(t, []recordedEvent{}, a.recordedEvents(t), "no invitation recorded")
		})
	}
}

// Given a member, then creating an invitation answers 403 and creates none. Without a session, it
// answers 401.
func TestInvitations_OnlyAnAdminInvites(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	member := a.mustCreateUser(t, "alice", store.RoleMember)
	body := jsonBody(t, map[string]string{"username": "bob", "role": "admin"})

	rec := postAt(t, h, "http://example.com", invitationsPath, body, "verve_session="+a.mustCreateSession(t, member))
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`, rec.Body.String())

	rec = postAt(t, h, "http://example.com", invitationsPath, body)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.JSONEq(t, unauthorizedBody, rec.Body.String())

	require.Equal(t, []recordedEvent{}, a.recordedEvents(t))
}

// Given a valid token in the body, then POST /api/v1/invitations/inspect answers 200 with the
// username and the expiry of the invitation, without authentication.
func TestInvitations_InspectReturnsTheUsernameAndExpiry(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie, _ := a.adminSession(t)
	token := mustInvite(t, h, cookie, map[string]string{"username": "Bob", "email": "bob@example.com", "role": "member"})

	rec := postAt(t, h, "http://example.com", inspectInvitationPath, jsonBody(t, map[string]string{"token": token}))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"username":"Bob","expiresAt":"2026-11-07T08:00:00Z"}`, rec.Body.String())
}

// Given a valid token and a valid password, then POST /api/v1/invitations/accept creates the user
// with the invited username, email and role, opens their session, answers 201 with the user and the
// session cookie, and marks the invitation used. The user then logs in with the password.
func TestInvitations_AcceptCreatesTheUserAndOpensASession(t *testing.T) {
	t.Parallel()

	for _, role := range []store.UserRole{store.RoleMember, store.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			h := a.handler(t, "")
			cookie, _ := a.adminSession(t)
			token := mustInvite(t, h, cookie, map[string]string{"username": "Bob", "email": "bob@example.com", "role": string(role)})

			rec := postAt(t, h, "http://example.com", acceptInvitationPath,
				jsonBody(t, map[string]string{"token": token, "password": bobPassword}))

			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			want := fmt.Sprintf(`{"id":2,"username":"Bob","email":"bob@example.com","role":%q,"timezone":null}`, role)
			require.JSONEq(t, want, rec.Body.String())

			id := sessionCookieOf(t, rec, "verve_session")
			require.Equal(t, []string{"verve_session=" + id + "; Path=/; Expires=Fri, 04 Dec 2026 08:00:00 GMT; HttpOnly; SameSite=Lax"},
				rec.Header().Values("Set-Cookie"))

			me := getWith(t, h, "/api/v1/auth/me", "verve_session="+id)
			require.Equal(t, http.StatusOK, me.Code)
			require.JSONEq(t, want, me.Body.String())

			login := loginAt(t, h, laptopAddr, "bob", bobPassword)
			require.Equal(t, http.StatusOK, login.Code, login.Body.String())

			hash := sha256.Sum256([]byte(token))
			stored, err := a.store.Invitations().GetByTokenHash(t.Context(), hash[:])
			require.NoError(t, err)
			require.Equal(t, authTestNow, stored.UsedAt)
			require.Equal(t, int64(2), stored.UserID)
		})
	}
}

// invitationProblem is the problem of a used, expired or unknown invitation.
const invitationProblem = `{"type":"about:blank","title":"Gone","status":410,"code":"invitation_invalid"}`

// Given a token used already, expired or unknown, then inspecting and accepting it answer 410
// invitation_invalid, and accepting creates no user.
func TestInvitations_InvalidTokenAnswers410(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// token returns the token to send, from a valid one created at authTestNow.
		token func(t *testing.T, a authTest, h http.Handler, valid string) string
		users int64
	}{
		{
			name: "used",
			token: func(t *testing.T, _ authTest, h http.Handler, valid string) string {
				rec := postAt(t, h, "http://example.com", acceptInvitationPath,
					jsonBody(t, map[string]string{"token": valid, "password": bobPassword}))
				require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

				return valid
			},
			users: 2,
		},
		{
			name: "expired",
			token: func(_ *testing.T, a authTest, _ http.Handler, valid string) string {
				a.clock.Advance(72*time.Hour + time.Millisecond)

				return valid
			},
			users: 1,
		},
		{
			name:  "unknown",
			token: func(*testing.T, authTest, http.Handler, string) string { return "bm90LWFuLWludml0YXRpb24tdG9rZW4" },
			users: 1,
		},
		{
			name:  "empty",
			token: func(*testing.T, authTest, http.Handler, string) string { return "" },
			users: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			h := a.handler(t, "")
			cookie, _ := a.adminSession(t)
			valid := mustInvite(t, h, cookie, map[string]string{"username": "bob", "role": "member"})
			token := tt.token(t, a, h, valid)

			rec := postAt(t, h, "http://example.com", inspectInvitationPath, jsonBody(t, map[string]string{"token": token}))
			require.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
			require.JSONEq(t, invitationProblem, rec.Body.String())

			rec = postAt(t, h, "http://example.com", acceptInvitationPath,
				jsonBody(t, map[string]string{"token": token, "password": bobPassword}))
			requireProblem(t, rec, http.StatusGone, "invitation_invalid")
			a.requireUserCount(t, tt.users)
		})
	}
}

// Given a valid token and an invitation that is just valid, at its expiry, then it can still be
// accepted.
func TestInvitations_InvitationIsValidUntilItsExpiry(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie, _ := a.adminSession(t)
	token := mustInvite(t, h, cookie, map[string]string{"username": "bob", "role": "member"})

	a.clock.Advance(72 * time.Hour)

	rec := postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": bobPassword}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// Given a valid token and a password that breaks the rules, then POST /api/v1/invitations/accept
// answers 422 with the code of the rule and creates no user. The invitation stays valid.
func TestInvitations_WeakPasswordAnswers422AndKeepsTheInvitation(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie, _ := a.adminSession(t)
	token := mustInvite(t, h, cookie, map[string]string{"username": "bob", "role": "member"})

	rec := postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": "abcdefghijk"}))
	requireProblem(t, rec, http.StatusUnprocessableEntity, "password_too_short")
	a.requireUserCount(t, 1)

	rec = postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": bobPassword}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// Given an invitation whose username a user took since it was created, as with verve user create,
// then accepting it answers 409 username_taken and creates no user.
func TestInvitations_AcceptAfterTheUsernameWasTakenAnswers409(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie, _ := a.adminSession(t)
	token := mustInvite(t, h, cookie, map[string]string{"username": "bob", "role": "member"})

	_, err := a.store.Users().Create(t.Context(), store.NewUser{Username: "BOB", Role: store.RoleMember})
	require.NoError(t, err)

	rec := postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": bobPassword}))
	requireProblem(t, rec, http.StatusConflict, "username_taken")
	a.requireUserCount(t, 2)
}

// lookupBarrierStore wraps a real store, so that barrier holds its lookups of invitations outside a
// transaction. The counts of countBarrier stand for the lookups here.
type lookupBarrierStore struct {
	store.Store

	barrier *countBarrier
}

func (s lookupBarrierStore) Invitations() store.InvitationsRepository {
	return lookupBarrierInvitations{InvitationsRepository: s.Store.Invitations(), barrier: s.barrier}
}

type lookupBarrierInvitations struct {
	store.InvitationsRepository

	barrier *countBarrier
}

func (i lookupBarrierInvitations) GetByTokenHash(ctx context.Context, tokenHash []byte) (store.Invitation, error) {
	invitation, err := i.InvitationsRepository.GetByTokenHash(ctx, tokenHash)

	if i.barrier.counted.Add(1) <= i.barrier.holds {
		i.barrier.arrived <- struct{}{}
		<-i.barrier.release
	}

	return invitation, err
}

// Given two concurrent accepts of one invitation, which both read that it is unused, then only one
// creates the user: the other answers 410 invitation_invalid.
func TestInvitations_ConcurrentAcceptsCreateOneUser(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	cookie, _ := a.adminSession(t)
	token := mustInvite(t, a.handler(t, ""), cookie, map[string]string{"username": "bob", "role": "member"})
	body := jsonBody(t, map[string]string{"token": token, "password": bobPassword})

	barrier := newCountBarrier(2)
	deps := a.deps(t, "")
	deps.Store = lookupBarrierStore{Store: a.store, barrier: barrier}
	h := NewHandler(deps)

	requests := []*http.Request{
		sameOriginPost(t, "http://example.com", acceptInvitationPath, body, phoneAddr),
		sameOriginPost(t, "http://example.com", acceptInvitationPath, body, laptopAddr),
	}

	statuses := make(chan int, len(requests))
	for _, r := range requests {
		go func() { statuses <- serveWith(h, r).Code }()
	}

	barrier.waitForCounts(t, len(requests))
	close(barrier.release)

	got := []int{<-statuses, <-statuses}
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusGone}, got)
	a.requireUserCount(t, 2)
}

// Given an invitation created then accepted, then the audit log records user.invited with the admin
// as actor, and user.created with the new user as subject and the invitation as source. A rejected
// accept records nothing.
func TestInvitations_RecordsTheAuditEvents(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie, admin := a.adminSession(t)
	token := mustInvite(t, h, cookie, map[string]string{"username": "bob", "email": "bob@example.com", "role": "member"})

	rec := postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": "abcdefghijk"}))
	requireProblem(t, rec, http.StatusUnprocessableEntity, "password_too_short")

	rec = postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": bobPassword}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	requireProblem(t, postAt(t, h, "http://example.com", acceptInvitationPath,
		jsonBody(t, map[string]string{"token": token, "password": bobPassword})), http.StatusGone, "invitation_invalid")

	require.Equal(t, []recordedEvent{
		{Type: "user.invited", ActorID: admin.ID, ClientIP: "203.0.113.7", UserAgent: userAgent,
			Payload: `{"username":"bob","role":"member"}`},
		{Type: "user.created", SubjectID: 2, ClientIP: "203.0.113.7", UserAgent: userAgent,
			Payload: `{"role":"member","source":"invitation"}`},
	}, a.recordedEvents(t))
}
