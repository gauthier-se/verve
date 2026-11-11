package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// adminUsersPath is the path of the admin user list.
const adminUsersPath = "/api/v1/admin/users"

// userStatusPath and userRolePath return the paths that change the status and the role of the user
// of id.
func userStatusPath(id int64) string { return fmt.Sprintf("%s/%d/status", adminUsersPath, id) }
func userRolePath(id int64) string   { return fmt.Sprintf("%s/%d/role", adminUsersPath, id) }

// adminUsersTest holds an admin and a member, each with a session, on a handler without a public
// URL.
type adminUsersTest struct {
	authTest

	handler     http.Handler
	admin       store.User
	adminCookie string
	bob         store.User
	bobCookie   string
}

func newAdminUsersTest(t *testing.T) adminUsersTest {
	t.Helper()

	a := newAuthTest(t)
	adminCookie, admin := a.adminSession(t)
	bob := a.mustCreateUser(t, "bob", store.RoleMember)

	return adminUsersTest{
		authTest:    a,
		handler:     a.handler(t, ""),
		admin:       admin,
		adminCookie: adminCookie,
		bob:         bob,
		bobCookie:   "verve_session=" + a.mustCreateSession(t, bob),
	}
}

// Given an admin, then GET /api/v1/admin/users lists every user newest first with their username,
// email, role, status, created date and last login, which is left out when there was none, and
// nothing else: nothing about health.
func TestAdminUsers_AdminListsTheMetadataOfTheUsers(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	u.clock.Advance(time.Hour)
	carol, err := u.store.Users().Create(t.Context(), store.NewUser{Username: "carol", Role: store.RoleMember})
	require.NoError(t, err)

	_, err = u.store.Users().SetStatus(t.Context(), carol.ID, store.UserDisabled)
	require.NoError(t, err)

	u.clock.Advance(time.Hour)
	_, err = u.store.Users().RecordLogin(t.Context(), u.bob.ID)
	require.NoError(t, err)

	rec := getWith(t, u.handler, adminUsersPath, u.adminCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"items":[
		{"id":%d,"username":"carol","role":"member","status":"disabled","createdAt":"2026-11-04T09:00:00Z","activeTokens":0},
		{"id":%d,"username":"bob","email":"bob@example.com","role":"member","status":"active",
		 "createdAt":"2026-11-04T08:00:00Z","lastLoginAt":"2026-11-04T10:00:00Z","activeTokens":0},
		{"id":%d,"username":"admin","email":"admin@example.com","role":"admin","status":"active",
		 "createdAt":"2026-11-04T08:00:00Z","activeTokens":0}
	]}`, carol.ID, u.bob.ID, u.admin.ID), rec.Body.String())
}

// Given a member, then the admin user operations answer 403 forbidden and change nothing. Given no
// session, then 401 unauthorized.
func TestAdminUsers_MemberIsForbiddenAndNoSessionIsUnauthorized(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	forbidden := `{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`

	rec := getWith(t, u.handler, adminUsersPath, u.bobCookie)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.JSONEq(t, forbidden, rec.Body.String())

	rec = accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.admin.ID), `{"status":"disabled"}`, u.bobCookie)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.JSONEq(t, forbidden, rec.Body.String())

	rec = accountRequest(t, u.handler, http.MethodPut, userRolePath(u.bob.ID), `{"role":"admin"}`, u.bobCookie)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.JSONEq(t, forbidden, rec.Body.String())

	stored, err := u.store.Users().Get(t.Context(), u.bob.ID)
	require.NoError(t, err)
	require.Equal(t, store.RoleMember, stored.Role)

	stored, err = u.store.Users().Get(t.Context(), u.admin.ID)
	require.NoError(t, err)
	require.Equal(t, store.UserActive, stored.Status)

	for _, rec := range []*http.Response{
		getWith(t, u.handler, adminUsersPath).Result(),
		accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.bob.ID), `{"status":"disabled"}`).Result(),
		accountRequest(t, u.handler, http.MethodPut, userRolePath(u.bob.ID), `{"role":"admin"}`).Result(),
	} {
		require.Equal(t, http.StatusUnauthorized, rec.StatusCode)
		require.NoError(t, rec.Body.Close())
	}
}

// Given an admin disabling a user, then PUT /api/v1/admin/users/{id}/status answers 200 with the
// user, whose session stops working at once, and records the event with the admin as actor and the
// client of the request. Enabling them answers 200 with the active user.
func TestAdminUsers_StatusChangeAnswersTheUserAndRevokesTheirSessions(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.bob.ID), `{"status":"disabled"}`, u.adminCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"username":"bob","email":"bob@example.com","role":"member",
		"status":"disabled","createdAt":"2026-11-04T08:00:00Z"}`, u.bob.ID), rec.Body.String())

	require.Equal(t, http.StatusUnauthorized, getWith(t, u.handler, "/api/v1/auth/me", u.bobCookie).Code)

	events, err := u.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: audit.TypeUserDisabled},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, u.admin.ID, events[0].ActorID)
	require.Equal(t, u.bob.ID, events[0].SubjectID)
	require.Equal(t, "203.0.113.7", events[0].ClientIP)

	rec = accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.bob.ID), `{"status":"active"}`, u.adminCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `"active"`, mustField(t, rec.Body.Bytes(), "status"))
}

// Given an admin changing a role, then PUT /api/v1/admin/users/{id}/role answers 200 with the user
// in their new role, and the session of the user stops working, so that they log in again.
func TestAdminUsers_RoleChangeAnswersTheUserAndRevokesTheirSessions(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	rec := accountRequest(t, u.handler, http.MethodPut, userRolePath(u.bob.ID), `{"role":"admin"}`, u.adminCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `"admin"`, mustField(t, rec.Body.Bytes(), "role"))

	require.Equal(t, http.StatusUnauthorized, getWith(t, u.handler, "/api/v1/auth/me", u.bobCookie).Code)
}

// Given the last active admin, then demoting or disabling them answers 409 last_admin.
func TestAdminUsers_LastActiveAdminAnswers409(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	requireProblem(t, accountRequest(t, u.handler, http.MethodPut, userRolePath(u.admin.ID), `{"role":"member"}`, u.adminCookie),
		http.StatusConflict, "last_admin")
	requireProblem(t, accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.admin.ID), `{"status":"disabled"}`, u.adminCookie),
		http.StatusConflict, "last_admin")
}

// Given an unknown user, then the status and role changes answer 404 not_found. Given a status or a
// role outside the enum, then 422.
func TestAdminUsers_UnknownUserAnswers404AndUnknownValues422(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	requireProblem(t, accountRequest(t, u.handler, http.MethodPut, userStatusPath(42), `{"status":"disabled"}`, u.adminCookie),
		http.StatusNotFound, "not_found")
	requireProblem(t, accountRequest(t, u.handler, http.MethodPut, userRolePath(42), `{"role":"admin"}`, u.adminCookie),
		http.StatusNotFound, "not_found")

	rec := accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.bob.ID), `{"status":"deleting"}`, u.adminCookie)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	rec = accountRequest(t, u.handler, http.MethodPut, userRolePath(u.bob.ID), `{"role":"owner"}`, u.adminCookie)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// mustField returns the JSON of the field name of the JSON object body.
func mustField(t *testing.T, body []byte, name string) string {
	t.Helper()

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))

	field, ok := fields[name]
	require.True(t, ok, name)

	return string(field)
}

// Given an account being deleted, then its status and role changes answer 409 user_deleting.
func TestAdminUsers_AccountBeingDeletedAnswers409(t *testing.T) {
	t.Parallel()

	u := newAdminUsersTest(t)

	_, err := u.store.Users().SetStatus(t.Context(), u.bob.ID, store.UserDeleting)
	require.NoError(t, err)

	requireProblem(t, accountRequest(t, u.handler, http.MethodPut, userStatusPath(u.bob.ID), `{"status":"active"}`, u.adminCookie),
		http.StatusConflict, "user_deleting")
	requireProblem(t, accountRequest(t, u.handler, http.MethodPut, userRolePath(u.bob.ID), `{"role":"admin"}`, u.adminCookie),
		http.StatusConflict, "user_deleting")
}
