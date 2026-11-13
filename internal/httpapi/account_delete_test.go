package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
)

// accountPath is the path of the account of the user of the session.
const accountPath = "/api/v1/account"

// adminUserPath returns the path of the user of id in the admin area.
func adminUserPath(id int64) string { return fmt.Sprintf("%s/%d", adminUsersPath, id) }

// clearedCookie is the session cookie that a response clears, on an instance without a public URL.
const clearedCookie = "verve_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"

// requireDeletionPending checks that the user of id is being deleted, and that the account.delete
// job of that user is queued.
func requireDeletionPending(t *testing.T, s store.Store, id int64) {
	t.Helper()

	user, err := s.Users().Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, store.UserDeleting, user.Status)

	job, _, err := s.Jobs().Claim(t.Context(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, auth.KindDeleteAccount, job.Kind)

	var payload auth.DeleteAccountPayload
	require.NoError(t, json.Unmarshal(job.Payload, &payload))
	require.Equal(t, id, payload.UserID)
}

// Given the right password, then DELETE /api/v1/account answers 202, clears the session cookie,
// revokes the sessions of the user at once and queues the deletion, recorded with the client of
// the request.
func TestAccountDelete_RightPasswordAnswers202AndDisablesTheAccount(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)
	otherCookie := a.mustCreateCookie(t, a.alice)

	rec := accountRequest(t, a.handler, http.MethodDelete, accountPath,
		jsonBody(t, map[string]string{"password": alicePassword}), a.aliceCookie)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Empty(t, rec.Body.String())
	require.Equal(t, []string{clearedCookie}, rec.Header().Values("Set-Cookie"))

	for _, cookie := range []string{a.aliceCookie, otherCookie} {
		require.Equal(t, http.StatusUnauthorized, getWith(t, a.handler, mePath, cookie).Code)
	}

	require.Equal(t, http.StatusOK, getWith(t, a.handler, mePath, a.bobCookie).Code, "other users keep their sessions")

	requireDeletionPending(t, a.store, a.alice.ID)

	events, err := a.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: audit.TypeUserDeletionRequested},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, a.alice.ID, events[0].ActorID)
	require.Equal(t, a.alice.ID, events[0].SubjectID)
	require.Equal(t, "203.0.113.7", events[0].ClientIP)
}

// Given a wrong password, then DELETE /api/v1/account answers 403 invalid_credentials and the
// account stays active, with its session.
func TestAccountDelete_WrongPasswordAnswers403(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)

	rec := accountRequest(t, a.handler, http.MethodDelete, accountPath,
		jsonBody(t, map[string]string{"password": wrongPassword}), a.aliceCookie)

	requireProblem(t, rec, http.StatusForbidden, "invalid_credentials")

	require.Equal(t, http.StatusOK, getWith(t, a.handler, mePath, a.aliceCookie).Code)

	user, err := a.store.Users().Get(t.Context(), a.alice.ID)
	require.NoError(t, err)
	require.Equal(t, store.UserActive, user.Status)
}

// Given the last active admin, then DELETE /api/v1/account answers 409 last_admin. Given no
// session, then 401.
func TestAccountDelete_LastAdminAnswers409AndNoSessionAnswers401(t *testing.T) {
	t.Parallel()

	a := newAccountTest(t)

	_, err := a.store.Users().SetRole(t.Context(), a.alice.ID, store.RoleAdmin)
	require.NoError(t, err)

	rec := accountRequest(t, a.handler, http.MethodDelete, accountPath,
		jsonBody(t, map[string]string{"password": alicePassword}), a.aliceCookie)
	requireProblem(t, rec, http.StatusConflict, "last_admin")

	rec = accountRequest(t, a.handler, http.MethodDelete, accountPath, jsonBody(t, map[string]string{"password": alicePassword}))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// Given an admin and the exact username as confirmation, then DELETE /api/v1/admin/users/{id}
// answers 202 with the user, being deleted, whose sessions stop working at once.
func TestAdminUserDelete_ExactUsernameAnswers202WithTheUserBeingDeleted(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := accountRequest(t, u.handler, http.MethodDelete, adminUserPath(u.bob.ID),
		jsonBody(t, map[string]string{"confirmation": "bob"}), u.adminCookie)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"username":"bob","email":"bob@example.com","role":"member",
		"status":"deleting","createdAt":"2026-11-04T08:00:00Z","activeTokens":0,"samples":0,"storageBytes":0,"lastIngestions":{}}`, u.bob.ID), rec.Body.String())
	require.Empty(t, rec.Header().Values("Set-Cookie"))

	require.Equal(t, http.StatusUnauthorized, getWith(t, u.handler, mePath, u.bobCookie).Code)
	requireDeletionPending(t, u.store, u.bob.ID)

	events, err := u.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: audit.TypeUserDeletionRequested},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, u.admin.ID, events[0].ActorID)
	require.Equal(t, u.bob.ID, events[0].SubjectID)

	rec = accountRequest(t, u.handler, http.MethodDelete, adminUserPath(u.bob.ID),
		jsonBody(t, map[string]string{"confirmation": "bob"}), u.adminCookie)
	requireProblem(t, rec, http.StatusConflict, "user_deleting")
}

// Given a confirmation that is not the exact username, then 422 confirmation_mismatch. Given the
// last active admin, then 409 last_admin. Given an unknown user, then 404. Nothing changes.
func TestAdminUserDelete_RefusedDeletionsChangeNothing(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := accountRequest(t, u.handler, http.MethodDelete, adminUserPath(u.bob.ID),
		jsonBody(t, map[string]string{"confirmation": "Bob"}), u.adminCookie)
	requireProblem(t, rec, http.StatusUnprocessableEntity, "confirmation_mismatch")

	rec = accountRequest(t, u.handler, http.MethodDelete, adminUserPath(u.admin.ID),
		jsonBody(t, map[string]string{"confirmation": "admin"}), u.adminCookie)
	requireProblem(t, rec, http.StatusConflict, "last_admin")

	rec = accountRequest(t, u.handler, http.MethodDelete, adminUserPath(42),
		jsonBody(t, map[string]string{"confirmation": "bob"}), u.adminCookie)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	for _, user := range []store.User{u.bob, u.admin} {
		stored, err := u.store.Users().Get(t.Context(), user.ID)
		require.NoError(t, err)
		require.Equal(t, store.UserActive, stored.Status)
	}

	require.Equal(t, http.StatusOK, getWith(t, u.handler, mePath, u.bobCookie).Code)
}

// Given a member, then DELETE /api/v1/admin/users/{id} answers 403 forbidden and changes nothing.
func TestAdminUserDelete_MemberIsForbidden(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := accountRequest(t, u.handler, http.MethodDelete, adminUserPath(u.admin.ID),
		jsonBody(t, map[string]string{"confirmation": "admin"}), u.bobCookie)
	requireProblem(t, rec, http.StatusForbidden, "forbidden")

	stored, err := u.store.Users().Get(t.Context(), u.admin.ID)
	require.NoError(t, err)
	require.Equal(t, store.UserActive, stored.Status)
}
