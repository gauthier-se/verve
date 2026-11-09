package auth

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// adminTest is a loginTest with the admin service on its hasher and sessions.
type adminTest struct {
	loginTest

	admin Admin
}

func newAdminTest(t *testing.T) adminTest {
	t.Helper()

	lt := newLoginTest(t)

	return adminTest{loginTest: lt, admin: NewAdmin(NewAccounts(lt.hasher, NewPasswordPolicy()), lt.sessions)}
}

// mustCreateRole stores an active user named username with role, whose password is alicePassword.
func (at adminTest) mustCreateRole(t *testing.T, username string, role store.UserRole) store.User {
	t.Helper()

	user := at.mustCreateUser(t, username, currentHash(alicePassword))
	if role == store.RoleMember {
		return user
	}

	user, err := at.store.Users().SetRole(t.Context(), user.ID, role)
	require.NoError(t, err)

	return user
}

// mustLogIn logs user in with alicePassword, from the phone, and returns the identifier of the
// session.
func (at adminTest) mustLogIn(t *testing.T, user store.User) string {
	t.Helper()

	loggedIn, err := at.login(t.Context(), phoneIP, user.Username, alicePassword)
	require.NoError(t, err)

	return loggedIn.SessionID
}

// requireSessionCount checks that user has want sessions.
func (at adminTest) requireSessionCount(t *testing.T, user store.User, want int) {
	t.Helper()

	sessions, err := at.store.Sessions().ListByUser(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, want)
}

// eventsOfType returns the audit events of eventType, newest first.
func (at adminTest) eventsOfType(t *testing.T, eventType string) []store.AuditEvent {
	t.Helper()

	events, err := at.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: eventType}, store.PageRequest{Limit: 50})
	require.NoError(t, err)

	return events
}

// Given an admin disabling a user, then the sessions of the user are revoked at once, and their
// login fails until an admin enables them again. Both actions record their audit event, with the
// admin as actor and the user as subject.
func TestAdmin_DisablingRevokesTheSessionsAndLoginFailsUntilEnabled(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	admin := at.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)
	session := at.mustLogIn(t, bob)
	at.mustLogIn(t, bob)
	adminSession := at.mustLogIn(t, admin)

	disabled, err := at.admin.SetStatus(t.Context(), at.store, StatusChange{ActorID: admin.ID, UserID: bob.ID, Status: store.UserDisabled})
	require.NoError(t, err)
	require.Equal(t, store.UserDisabled, disabled.Status)

	at.requireSessionCount(t, bob, 0)

	_, err = at.sessions.Lookup(t.Context(), at.store, session)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = at.sessions.Lookup(t.Context(), at.store, adminSession)
	require.NoError(t, err, "the sessions of other users stay")

	_, err = at.login(t.Context(), phoneIP, "bob", alicePassword)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	enabled, err := at.admin.SetStatus(t.Context(), at.store, StatusChange{ActorID: admin.ID, UserID: bob.ID, Status: store.UserActive})
	require.NoError(t, err)
	require.Equal(t, store.UserActive, enabled.Status)

	_, err = at.login(t.Context(), phoneIP, "bob", alicePassword)
	require.NoError(t, err)

	for _, eventType := range []string{audit.TypeUserDisabled, audit.TypeUserEnabled} {
		events := at.eventsOfType(t, eventType)
		require.Len(t, events, 1, eventType)
		require.Equal(t, admin.ID, events[0].ActorID, eventType)
		require.Equal(t, bob.ID, events[0].SubjectID, eventType)
	}
}

// Given a status that the user already has, then nothing changes: the sessions stay and no event
// is recorded.
func TestAdmin_SameStatusChangesNothing(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	admin := at.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)
	at.mustLogIn(t, bob)

	user, err := at.admin.SetStatus(t.Context(), at.store, StatusChange{ActorID: admin.ID, UserID: bob.ID, Status: store.UserActive})
	require.NoError(t, err)
	require.Equal(t, store.UserActive, user.Status)

	at.requireSessionCount(t, bob, 1)
	require.Empty(t, at.eventsOfType(t, audit.TypeUserEnabled))
}

// Given a role change, then the sessions of the user are revoked, so that they log in again with a
// new session, and the change records its audit event with the former and the new role. Given the
// role that the user already has, then nothing changes.
func TestAdmin_RoleChangeRevokesTheSessionsOfTheUser(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	admin := at.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)
	session := at.mustLogIn(t, bob)

	promoted, err := at.admin.SetRole(t.Context(), at.store, RoleChange{ActorID: admin.ID, UserID: bob.ID, Role: store.RoleAdmin})
	require.NoError(t, err)
	require.Equal(t, store.RoleAdmin, promoted.Role)

	_, err = at.sessions.Lookup(t.Context(), at.store, session)
	require.ErrorIs(t, err, store.ErrNotFound)

	events := at.eventsOfType(t, audit.TypeUserRoleChanged)
	require.Len(t, events, 1)
	require.Equal(t, admin.ID, events[0].ActorID)
	require.Equal(t, bob.ID, events[0].SubjectID)
	require.JSONEq(t, `{"from":"member","to":"admin"}`, string(events[0].Payload))

	at.mustLogIn(t, bob)

	_, err = at.admin.SetRole(t.Context(), at.store, RoleChange{ActorID: admin.ID, UserID: bob.ID, Role: store.RoleAdmin})
	require.NoError(t, err)

	at.requireSessionCount(t, bob, 1)
	require.Len(t, at.eventsOfType(t, audit.TypeUserRoleChanged), 1)
}

// Given the last active admin, then demoting or disabling them fails with ErrLastAdmin, also when a
// disabled admin exists, and changes nothing: their role, status and sessions stay, and no event is
// recorded. Given another active admin, then the same change succeeds.
func TestAdmin_LastActiveAdminCannotBeDemotedOrDisabled(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	admin := at.mustCreateRole(t, "admin", store.RoleAdmin)
	formerAdmin := at.mustCreateRole(t, "former", store.RoleAdmin)
	at.mustLogIn(t, admin)

	_, err := at.admin.SetStatus(t.Context(), at.store, StatusChange{ActorID: admin.ID, UserID: formerAdmin.ID, Status: store.UserDisabled})
	require.NoError(t, err)

	_, err = at.admin.SetRole(t.Context(), at.store, RoleChange{ActorID: admin.ID, UserID: admin.ID, Role: store.RoleMember})
	require.ErrorIs(t, err, ErrLastAdmin)

	_, err = at.admin.SetStatus(t.Context(), at.store, StatusChange{UserID: admin.ID, Status: store.UserDisabled})
	require.ErrorIs(t, err, ErrLastAdmin)

	stored, err := at.store.Users().Get(t.Context(), admin.ID)
	require.NoError(t, err)
	require.Equal(t, store.RoleAdmin, stored.Role)
	require.Equal(t, store.UserActive, stored.Status)
	at.requireSessionCount(t, admin, 1)
	require.Empty(t, at.eventsOfType(t, audit.TypeUserRoleChanged))
	require.Len(t, at.eventsOfType(t, audit.TypeUserDisabled), 1, "only the disabling of the former admin")

	other := at.mustCreateRole(t, "other", store.RoleAdmin)

	_, err = at.admin.SetRole(t.Context(), at.store, RoleChange{ActorID: other.ID, UserID: admin.ID, Role: store.RoleMember})
	require.NoError(t, err)
}

// countBarrier wraps a real store, so that the lookups of the number of active admins outside a
// transaction wait until both concurrent changes made theirs: as when both requests read the count
// before either writes.
type countBarrier struct {
	store.Store

	arrived *sync.WaitGroup
}

func (s countBarrier) Users() store.UsersRepository {
	return countBarrierUsers{UsersRepository: s.Store.Users(), arrived: s.arrived}
}

type countBarrierUsers struct {
	store.UsersRepository

	arrived *sync.WaitGroup
}

func (u countBarrierUsers) CountActiveAdmins(ctx context.Context) (int64, error) {
	u.arrived.Done()
	u.arrived.Wait()

	return u.UsersRepository.CountActiveAdmins(ctx)
}

// Given the last two active admins demoted at the same time, then one demotion succeeds and the
// other fails with ErrLastAdmin: the check of the last admin runs in the transaction of the write,
// so that one active admin is left.
func TestAdmin_ConcurrentDemotionsOfTheLastTwoAdminsLeaveOne(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	alice := at.mustCreateRole(t, "alice", store.RoleAdmin)
	bob := at.mustCreateRole(t, "bob", store.RoleAdmin)

	var arrived sync.WaitGroup
	arrived.Add(2)

	st := countBarrier{Store: at.store, arrived: &arrived}
	errs := make([]error, 2)

	var wg sync.WaitGroup

	for i, user := range []store.User{alice, bob} {
		wg.Go(func() {
			_, errs[i] = at.admin.SetRole(t.Context(), st, RoleChange{ActorID: user.ID, UserID: user.ID, Role: store.RoleMember})
		})
	}

	wg.Wait()

	require.ElementsMatch(t, []bool{true, false}, []bool{errs[0] == nil, errs[1] == nil}, "errors: %v", errs)

	for _, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, ErrLastAdmin)
		}
	}

	count, err := at.store.Users().CountActiveAdmins(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

// Given an unknown user, then a status or role change fails with store.ErrNotFound.
func TestAdmin_UnknownUserIsNotFound(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)

	_, err := at.admin.SetStatus(t.Context(), at.store, StatusChange{UserID: 42, Status: store.UserDisabled})
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = at.admin.SetRole(t.Context(), at.store, RoleChange{UserID: 42, Role: store.RoleAdmin})
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Given a password reset by the system, then the user logs in with the new password only, their
// sessions are revoked, and the reset records its audit event with the system as actor.
func TestAdmin_ResetPasswordReplacesItAndRevokesTheSessions(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)
	at.mustLogIn(t, bob)

	require.NoError(t, at.admin.ResetPassword(t.Context(), at.store, PasswordReset{UserID: bob.ID, Password: newPassword}))

	at.requireSessionCount(t, bob, 0)

	_, err := at.login(t.Context(), phoneIP, "bob", alicePassword)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	_, err = at.login(t.Context(), phoneIP, "bob", newPassword)
	require.NoError(t, err)

	events := at.eventsOfType(t, audit.TypePasswordReset)
	require.Len(t, events, 1)
	require.Zero(t, events[0].ActorID)
	require.Equal(t, bob.ID, events[0].SubjectID)
}

// Given a new password that breaks the policy, then the reset fails with the error of the rule and
// changes nothing.
func TestAdmin_ResetPasswordChecksThePolicy(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)
	at.mustLogIn(t, bob)

	err := at.admin.ResetPassword(t.Context(), at.store, PasswordReset{UserID: bob.ID, Password: "short"})
	require.ErrorIs(t, err, ErrPasswordTooShort)

	at.requireSessionCount(t, bob, 1)
	require.Empty(t, at.eventsOfType(t, audit.TypePasswordReset))

	err = at.admin.ResetPassword(t.Context(), at.store, PasswordReset{UserID: 42, Password: newPassword})
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Given an account being deleted, then its status and role cannot change, so that enabling it does
// not cancel the deletion: ErrUserDeleting, and nothing changes.
func TestAdmin_AccountBeingDeletedCannotChange(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	admin := at.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)

	_, err := at.store.Users().SetStatus(t.Context(), bob.ID, store.UserDeleting)
	require.NoError(t, err)

	for _, status := range []store.UserStatus{store.UserActive, store.UserDisabled} {
		_, err = at.admin.SetStatus(t.Context(), at.store, StatusChange{ActorID: admin.ID, UserID: bob.ID, Status: status})
		require.ErrorIs(t, err, ErrUserDeleting, status)
	}

	_, err = at.admin.SetRole(t.Context(), at.store, RoleChange{ActorID: admin.ID, UserID: bob.ID, Role: store.RoleAdmin})
	require.ErrorIs(t, err, ErrUserDeleting)

	stored, err := at.store.Users().Get(t.Context(), bob.ID)
	require.NoError(t, err)
	require.Equal(t, store.UserDeleting, stored.Status)
	require.Equal(t, store.RoleMember, stored.Role)
}

// Given a status or a role that does not exist, then the change fails with store.ErrInvalid and
// stores nothing.
func TestAdmin_UnknownStatusOrRoleIsInvalid(t *testing.T) {
	t.Parallel()

	at := newAdminTest(t)
	bob := at.mustCreateRole(t, "bob", store.RoleMember)

	_, err := at.admin.SetStatus(t.Context(), at.store, StatusChange{UserID: bob.ID, Status: store.UserDeleting})
	require.ErrorIs(t, err, store.ErrInvalid)

	_, err = at.admin.SetRole(t.Context(), at.store, RoleChange{UserID: bob.ID, Role: "owner"})
	require.ErrorIs(t, err, store.ErrInvalid)

	stored, err := at.store.Users().Get(t.Context(), bob.ID)
	require.NoError(t, err)
	require.Equal(t, bob, stored)
}
