package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// passwordResetPath is the path that completes a password reset with the token of its link.
const passwordResetPath = "/api/v1/password-reset"

// bobNewPassword is the password that bob chooses through a reset link.
const bobNewPassword = "a new passphrase for bob" //nolint:gosec // G101: a fake password of the tests

// resetLinkPath returns the path that creates a reset link for the user of id.
func resetLinkPath(id int64) string { return fmt.Sprintf("%s/%d/password-reset", adminUsersPath, id) }

// createdResetLink is the response of a created reset link.
type createdResetLink struct {
	UserID    int64     `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	Link      string    `json:"link"`
}

// mustCreateResetLink creates on u.handler a reset link for the user of id as the admin of u, and
// returns its token.
func (u adminUsersTest) mustCreateResetLink(t *testing.T, id int64) string {
	t.Helper()

	rec := postAt(t, u.handler, "http://example.com", resetLinkPath(id), "", u.adminCookie)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var created createdResetLink
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	token, found := strings.CutPrefix(created.Link, "/reset-password#")
	require.True(t, found, created.Link)

	return token
}

// resetPassword completes on u.handler the reset of the link of token with password.
func (u adminUsersTest) resetPassword(t *testing.T, token, password string) *http.Response {
	t.Helper()

	rec := postAt(t, u.handler, "http://example.com", passwordResetPath,
		jsonBody(t, map[string]string{"token": token, "password": password}))

	return rec.Result()
}

// Given an admin, then POST /api/v1/admin/users/{id}/password-reset answers 201 with a single-use
// link valid 24 hours, once. Only the hash of the token is stored, as a reset link of the user.
func TestPasswordReset_AdminCreatesALinkValid24Hours(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := postAt(t, u.handler, "http://example.com", resetLinkPath(u.bob.ID), "", u.adminCookie)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var created createdResetLink
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	token, found := strings.CutPrefix(created.Link, "/reset-password#")
	require.True(t, found, created.Link)
	require.Len(t, token, 43, "256 random bits in unpadded base64url")
	require.JSONEq(t, fmt.Sprintf(`{"userId":%d,"expiresAt":"2026-11-05T08:00:00Z","link":%q}`, u.bob.ID, created.Link),
		rec.Body.String())

	hash := sha256.Sum256([]byte(token))
	stored, err := u.store.Invitations().GetByTokenHash(t.Context(), hash[:])
	require.NoError(t, err)
	require.Equal(t, store.Invitation{
		ID:        stored.ID,
		Kind:      store.InvitationReset,
		ExpiresAt: authTestNow.Add(24 * time.Hour),
		UserID:    u.bob.ID,
		CreatedBy: u.admin.ID,
		CreatedAt: authTestNow,
	}, stored)
}

// Given a public URL, then the reset link is <VERVE_PUBLIC_URL>/reset-password#<token>. Given none,
// then it is the path /reset-password#<token>, to which the client prepends its origin.
func TestPasswordReset_LinkStartsWithThePublicURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		publicURL string
		origin    string
		cookie    string
		prefix    string
	}{
		{name: "public URL", publicURL: "https://verve.example.com", origin: "https://verve.example.com",
			cookie: "__Host-verve_session", prefix: "https://verve.example.com/reset-password#"},
		{name: "no public URL", origin: "http://example.com", cookie: "verve_session", prefix: "/reset-password#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthTest(t)
			h := a.handler(t, tt.publicURL)
			admin := a.mustCreateUser(t, "admin", store.RoleAdmin)
			bob := a.mustCreateUser(t, "bob", store.RoleMember)
			cookie := tt.cookie + "=" + a.mustCreateSession(t, admin)

			rec := postAt(t, h, tt.origin, resetLinkPath(bob.ID), "", cookie)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

			var created createdResetLink
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

			token, found := strings.CutPrefix(created.Link, tt.prefix)
			require.True(t, found, created.Link)
			require.Len(t, token, 43)
		})
	}
}

// Given a member, then creating a reset link answers 403 forbidden. Without a session, 401. Given
// an unknown user, then 404 not_found, and an account being deleted 409 user_deleting. None
// creates a link.
func TestPasswordReset_OnlyAnAdminCreatesALinkForAnExistingUser(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := postAt(t, u.handler, "http://example.com", resetLinkPath(u.admin.ID), "", u.bobCookie)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`, rec.Body.String())

	rec = postAt(t, u.handler, "http://example.com", resetLinkPath(u.bob.ID), "")
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.JSONEq(t, unauthorizedBody, rec.Body.String())

	requireProblem(t, postAt(t, u.handler, "http://example.com", resetLinkPath(42), "", u.adminCookie),
		http.StatusNotFound, "not_found")

	_, err := u.store.Users().SetStatus(t.Context(), u.bob.ID, store.UserDeleting)
	require.NoError(t, err)

	requireProblem(t, postAt(t, u.handler, "http://example.com", resetLinkPath(u.bob.ID), "", u.adminCookie),
		http.StatusConflict, "user_deleting")

	require.Equal(t, []recordedEvent{}, u.recordedEvents(t))
}

// Given a new link for the same user, then the earlier links stop working, and the new one works.
func TestPasswordReset_NewLinkRevokesTheEarlierOnes(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)
	first := u.mustCreateResetLink(t, u.bob.ID)
	second := u.mustCreateResetLink(t, u.bob.ID)
	carol := u.mustCreateUser(t, "carol", store.RoleMember)
	carolToken := u.mustCreateResetLink(t, carol.ID)
	third := u.mustCreateResetLink(t, u.bob.ID)

	for _, token := range []string{first, second} {
		res := u.resetPassword(t, token, bobNewPassword)
		require.Equal(t, http.StatusGone, res.StatusCode)
		require.NoError(t, res.Body.Close())
	}

	for _, token := range []string{third, carolToken} {
		res := u.resetPassword(t, token, bobNewPassword)
		require.Equal(t, http.StatusNoContent, res.StatusCode, "the link of another user stays valid")
		require.NoError(t, res.Body.Close())
	}
}

// Given a valid token and a valid password, then POST /api/v1/password-reset answers 204, changes
// the password and revokes every session of the user, without opening one. The user then logs in
// with the new password.
func TestPasswordReset_ValidTokenChangesThePasswordAndRevokesEverySession(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)
	otherCookie := "verve_session=" + u.mustCreateSession(t, u.bob)
	token := u.mustCreateResetLink(t, u.bob.ID)

	rec := postAt(t, u.handler, "http://example.com", passwordResetPath,
		jsonBody(t, map[string]string{"token": token, "password": bobNewPassword}))

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rec.Header().Values("Set-Cookie"), "a reset opens no session")

	for _, cookie := range []string{u.bobCookie, otherCookie} {
		require.Equal(t, http.StatusUnauthorized, getWith(t, u.handler, "/api/v1/auth/me", cookie).Code)
	}

	require.Equal(t, http.StatusOK, getWith(t, u.handler, "/api/v1/auth/me", u.adminCookie).Code,
		"the sessions of other users stay")

	login := loginAt(t, u.handler, laptopAddr, "bob", bobNewPassword)
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())

	hash := sha256.Sum256([]byte(token))
	stored, err := u.store.Invitations().GetByTokenHash(t.Context(), hash[:])
	require.NoError(t, err)
	require.Equal(t, authTestNow, stored.UsedAt)
}

// Given a valid token and a password that breaks the rules, then POST /api/v1/password-reset
// answers 422 with the code of the rule, and the link stays valid.
func TestPasswordReset_WeakPasswordAnswers422AndKeepsTheLink(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)
	token := u.mustCreateResetLink(t, u.bob.ID)

	rec := postAt(t, u.handler, "http://example.com", passwordResetPath,
		jsonBody(t, map[string]string{"token": token, "password": "abcdefghijk"}))
	requireProblem(t, rec, http.StatusUnprocessableEntity, "password_too_short")
	require.Equal(t, http.StatusOK, getWith(t, u.handler, "/api/v1/auth/me", u.bobCookie).Code,
		"a rejected reset revokes nothing")

	res := u.resetPassword(t, token, bobNewPassword)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

// resetLinkProblem is the problem of a used, expired or unknown reset link.
const resetLinkProblem = `{"type":"about:blank","title":"Gone","status":410,"code":"reset_link_invalid"}`

// Given a token used twice, expired, unknown, empty or the one of an invitation, then POST
// /api/v1/password-reset answers 410 reset_link_invalid and changes no password.
func TestPasswordReset_InvalidTokenAnswers410(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// token returns the token to send, from a valid one created at authTestNow.
		token func(t *testing.T, u adminUsersTest, valid string) string
	}{
		{
			name: "used twice",
			token: func(t *testing.T, u adminUsersTest, valid string) string {
				res := u.resetPassword(t, valid, "a first new passphrase")
				require.Equal(t, http.StatusNoContent, res.StatusCode)
				require.NoError(t, res.Body.Close())

				return valid
			},
		},
		{
			name: "expired",
			token: func(_ *testing.T, u adminUsersTest, valid string) string {
				u.clock.Advance(24*time.Hour + time.Millisecond)

				return valid
			},
		},
		{
			name:  "unknown",
			token: func(*testing.T, adminUsersTest, string) string { return "bm90LWEtcmVzZXQtbGluay10b2tlbg" },
		},
		{
			name:  "empty",
			token: func(*testing.T, adminUsersTest, string) string { return "" },
		},
		{
			name: "invitation",
			token: func(t *testing.T, u adminUsersTest, _ string) string {
				return mustInvite(t, u.handler, u.adminCookie, map[string]string{"username": "dave", "role": "member"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := newAdminUsersTest(t)
			valid := u.mustCreateResetLink(t, u.bob.ID)
			token := tt.token(t, u, valid)

			rec := postAt(t, u.handler, "http://example.com", passwordResetPath,
				jsonBody(t, map[string]string{"token": token, "password": bobNewPassword}))
			require.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
			require.JSONEq(t, resetLinkProblem, rec.Body.String())

			login := loginAt(t, u.handler, laptopAddr, "bob", bobNewPassword)
			require.Equal(t, http.StatusUnauthorized, login.Code, "the password is unchanged")
		})
	}
}

// Given a reset link that is just valid, at its expiry, then it can still be used. Given the token
// of a reset link for an inspection of an invitation, then 410 invitation_invalid.
func TestPasswordReset_LinkIsValidUntilItsExpiryAndIsNoInvitation(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)
	token := u.mustCreateResetLink(t, u.bob.ID)

	requireProblem(t, postAt(t, u.handler, "http://example.com", inspectInvitationPath,
		jsonBody(t, map[string]string{"token": token})), http.StatusGone, "invitation_invalid")

	u.clock.Advance(24 * time.Hour)

	res := u.resetPassword(t, token, bobNewPassword)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

// Given a body larger than 8 KiB, then POST /api/v1/password-reset answers 413.
func TestPasswordReset_LargeBodyAnswers413(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := postAt(t, u.handler, "http://example.com", passwordResetPath,
		jsonBody(t, map[string]string{"token": strings.Repeat("a", 8*1024), "password": bobNewPassword}))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
}

// Given a reset link created then used, then the audit log records user.password_reset_link_created
// with the admin as actor and the user as subject, and auth.password_reset_completed with the user
// as actor, both with the client of their request. A rejected reset records nothing.
func TestPasswordReset_RecordsTheAuditEvents(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)
	token := u.mustCreateResetLink(t, u.bob.ID)

	rec := postAt(t, u.handler, "http://example.com", passwordResetPath,
		jsonBody(t, map[string]string{"token": token, "password": "abcdefghijk"}))
	requireProblem(t, rec, http.StatusUnprocessableEntity, "password_too_short")

	res := u.resetPassword(t, token, bobNewPassword)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	require.NoError(t, res.Body.Close())

	res = u.resetPassword(t, token, bobNewPassword)
	require.Equal(t, http.StatusGone, res.StatusCode)
	require.NoError(t, res.Body.Close())

	require.Equal(t, []recordedEvent{
		{Type: "user.password_reset_link_created", ActorID: u.admin.ID, SubjectID: u.bob.ID,
			ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{}`},
		{Type: "auth.password_reset_completed", ActorID: u.bob.ID,
			ClientIP: "203.0.113.7", UserAgent: userAgent, Payload: `{}`},
	}, u.recordedEvents(t))
}
