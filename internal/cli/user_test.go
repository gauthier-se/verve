package cli

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// alicePassword is the password of the users of the tests.
const alicePassword = "correct horse battery staple"

// userDeps returns the dependencies of the command line on a migrated data directory, which it also
// returns, with the user creation of production.
func userDeps(t *testing.T) (Deps, string) {
	t.Helper()

	dataDir := t.TempDir()
	deps := depsWithEnv(map[string]string{"VERVE_DATA_DIR": dataDir})
	deps.CreateUser = app.CreateUser
	deps.ListUsers = app.ListUsers
	deps.SetUserStatus = app.SetUserStatus
	deps.ResetPassword = app.ResetPassword

	cfg, err := deps.LoadConfig("")
	require.NoError(t, err)

	_, err = app.Migrate(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return deps, dataDir
}

// openStore opens the store of the database in dataDir, next to the commands of the test.
func openStore(t *testing.T, dataDir string) store.Store {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(dataDir, "verve.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.CloseShared()) })

	return sqlite.New(db, clock.Real())
}

// Given verve user create --password-stdin, then it reads the password from stdin, without its final
// newline, and creates an active member, or an admin with --admin, with that password. It records the
// creation, without a client.
func TestUserCreate_PasswordStdinCreatesTheUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		stdin string
		role  store.UserRole
		out   string
	}{
		{
			name:  "member",
			args:  []string{"user", "create", "--password-stdin", "alice"},
			stdin: alicePassword + "\n",
			role:  store.RoleMember,
			out:   "Created member alice\n",
		},
		{
			name:  "admin",
			args:  []string{"user", "create", "--admin", "--password-stdin", "alice"},
			stdin: alicePassword + "\r\n",
			role:  store.RoleAdmin,
			out:   "Created admin alice\n",
		},
		{
			name:  "without a final newline",
			args:  []string{"user", "create", "--password-stdin", "alice"},
			stdin: alicePassword,
			role:  store.RoleMember,
			out:   "Created member alice\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps, dataDir := userDeps(t)

			res := runWithStdin(t, deps, tt.stdin, tt.args...)

			require.Equal(t, 0, res.code, res.stderr)
			require.Equal(t, tt.out, res.stdout)
			require.Empty(t, res.stderr)

			s := openStore(t, dataDir)

			user, err := s.Users().GetByLogin(t.Context(), "alice")
			require.NoError(t, err)
			require.Equal(t, tt.role, user.Role)
			require.Equal(t, store.UserActive, user.Status)

			ok, err := auth.DefaultHasher().Verify(user.PasswordHash, alicePassword)
			require.NoError(t, err)
			require.True(t, ok, "the user must have the password of stdin")

			events, err := s.AuditEvents().List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 10})
			require.NoError(t, err)
			require.Len(t, events, 1)
			require.Equal(t, "user.created", events[0].Type)
			require.Zero(t, events[0].ActorID)
			require.Equal(t, user.ID, events[0].SubjectID)
			require.Empty(t, events[0].ClientIP)
			require.JSONEq(t, `{"role":"`+string(tt.role)+`","source":"cli"}`, string(events[0].Payload))
		})
	}
}

// terminalAnswering returns a terminal that answers the password prompts with answers, in order.
func terminalAnswering(answers ...string) passwordFunc {
	return func() (string, error) {
		if len(answers) == 0 {
			return "", errors.New("no more answers")
		}

		answer := answers[0]
		answers = answers[1:]

		return answer, nil
	}
}

// requireNoUser checks that the database in dataDir holds no user.
func requireNoUser(t *testing.T, dataDir string) {
	t.Helper()

	count, err := openStore(t, dataDir).Users().Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

// Given verve user create on a terminal, then it asks the password twice on stderr and creates the
// user with it.
func TestUserCreate_TerminalAsksThePasswordTwice(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	deps.ReadPassword = terminalAnswering(alicePassword, alicePassword)

	res := run(t, deps, "user", "create", "--admin", "alice")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, "Password: \nConfirm password: \n", res.stderr)
	require.Equal(t, "Created admin alice\n", res.stdout)

	user, err := openStore(t, dataDir).Users().GetByLogin(t.Context(), "alice")
	require.NoError(t, err)

	ok, err := auth.DefaultHasher().Verify(user.PasswordHash, alicePassword)
	require.NoError(t, err)
	require.True(t, ok)
}

// Given two different passwords on the terminal, then verve user create fails with "passwords do not
// match" and creates no user.
func TestUserCreate_PasswordMismatchFails(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	deps.ReadPassword = terminalAnswering(alicePassword, alicePassword+"!")

	res := run(t, deps, "user", "create", "alice")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Password: \nConfirm password: \nError: passwords do not match\n", res.stderr)
	require.Empty(t, res.stdout)
	requireNoUser(t, dataDir)
}

// Given stdin that is not a terminal and no --password-stdin, then verve user create fails, tells to
// pass it, and creates no user.
func TestUserCreate_NoTerminalWithoutPasswordStdinFails(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)

	res := runWithStdin(t, deps, alicePassword+"\n", "user", "create", "alice")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: stdin is not a terminal: pass --password-stdin to read the password from it\n", res.stderr)
	requireNoUser(t, dataDir)
}

// Given a password that breaks the password policy, an invalid username or a taken one, then verve
// user create fails with what to change and creates no user.
func TestUserCreate_RefusesWhatBreaksTheRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
		want     string
	}{
		{name: "short password", username: "alice", password: "abcdefghijk",
			want: "Error: password must have at least 12 characters\n"},
		{name: "long password", username: "alice", password: strings.Repeat("é", 129),
			want: "Error: password must have at most 128 characters\n"},
		{name: "common password", username: "alice", password: "Password1234",
			want: "Error: password is too common: choose another one\n"},
		{name: "invalid username", username: "alice@example.com", password: alicePassword,
			want: "Error: username must have 1 to 64 characters, without @, whitespace, control or invisible characters\n"},
		{name: "username taken in another case", username: "BOB", password: alicePassword,
			want: "Error: username \"BOB\" is taken\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps, dataDir := userDeps(t)

			res := runWithStdin(t, deps, alicePassword, "user", "create", "--password-stdin", "bob")
			require.Equal(t, 0, res.code, res.stderr)

			res = runWithStdin(t, deps, tt.password, "user", "create", "--password-stdin", tt.username)

			require.Equal(t, 1, res.code)
			require.Equal(t, tt.want, res.stderr)

			count, err := openStore(t, dataDir).Users().Count(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(1), count, "only bob must exist")
		})
	}
}

// Given serve holds the data directory lock, then verve user create still creates the user, next to
// it, and leaves the lock with serve.
func TestUserCreate_RunsWhileTheLockIsHeld(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)

	lock, err := app.AcquireLock(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	res := runWithStdin(t, deps, alicePassword, "user", "create", "--admin", "--password-stdin", "alice")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, "Created admin alice\n", res.stdout)

	user, err := openStore(t, dataDir).Users().GetByLogin(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, store.RoleAdmin, user.Role)

	_, err = app.AcquireLock(dataDir)
	require.EqualError(t, err, "another Verve process is using "+dataDir)
}

// Given an invalid configuration, then verve user create fails before it asks the password.
func TestUserCreate_InvalidConfigFailsBeforeAskingThePassword(t *testing.T) {
	t.Parallel()

	deps := depsWithEnv(map[string]string{"VERVE_LOG_LEVEL": "verbose"})
	deps.ReadPassword = terminalAnswering()

	res := run(t, deps, "user", "create", "alice")

	require.Equal(t, 1, res.code)
	require.True(t, strings.HasPrefix(res.stderr, "Error: invalid configuration"), res.stderr)
}

// mustCreate creates through the command line, with alicePassword, the user of args, such as
// "--admin", "alice".
func mustCreate(t *testing.T, deps Deps, args ...string) {
	t.Helper()

	res := runWithStdin(t, deps, alicePassword, append([]string{"user", "create", "--password-stdin"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)
}

// mustOpenSession opens a session of the user named username in the database of dataDir.
func mustOpenSession(t *testing.T, dataDir, username string) {
	t.Helper()

	s := openStore(t, dataDir)

	user, err := s.Users().GetByLogin(t.Context(), username)
	require.NoError(t, err)

	_, _, err = auth.NewSessions(clock.Real()).Create(t.Context(), s, auth.NewSession{UserID: user.ID, Method: store.LoginPassword})
	require.NoError(t, err)
}

// requireUser returns the user named username in the database of dataDir, and checks that they have
// sessions sessions.
func requireUser(t *testing.T, dataDir, username string, sessions int) store.User {
	t.Helper()

	s := openStore(t, dataDir)

	user, err := s.Users().GetByLogin(t.Context(), username)
	require.NoError(t, err)

	got, err := s.Sessions().ListByUser(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, got, sessions)

	return user
}

// requireSystemEvent checks that the database of dataDir holds one event of eventType, with the
// system as actor, user as subject and no client.
func requireSystemEvent(t *testing.T, dataDir, eventType string, user store.User) {
	t.Helper()

	events, err := openStore(t, dataDir).AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: eventType},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1, eventType)
	require.Zero(t, events[0].ActorID)
	require.Equal(t, user.ID, events[0].SubjectID)
	require.Empty(t, events[0].ClientIP)
}

// Given verve user list, then it prints a table of the users, newest first, with their username,
// email, role, status, created date and last login, a dash standing for none.
func TestUserList_PrintsTheUsers(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	mustCreate(t, deps, "--admin", "alice")
	mustCreate(t, deps, "bob")

	s := openStore(t, dataDir)

	bob, err := s.Users().GetByLogin(t.Context(), "bob")
	require.NoError(t, err)

	bob, err = s.Users().RecordLogin(t.Context(), bob.ID)
	require.NoError(t, err)

	alice, err := s.Users().GetByLogin(t.Context(), "alice")
	require.NoError(t, err)

	res := run(t, deps, "user", "list")

	require.Equal(t, 0, res.code, res.stderr)
	require.Empty(t, res.stderr)

	lines := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n")
	require.Len(t, lines, 3, res.stdout)
	require.Equal(t, []string{"USERNAME", "EMAIL", "ROLE", "STATUS", "CREATED", "LAST", "LOGIN"}, strings.Fields(lines[0]))
	require.Equal(t, []string{"bob", "-", "member", "active", bob.CreatedAt.Format(time.RFC3339),
		bob.LastLoginAt.Format(time.RFC3339)}, strings.Fields(lines[1]))
	require.Equal(t, []string{"alice", "-", "admin", "active", alice.CreatedAt.Format(time.RFC3339), "-"},
		strings.Fields(lines[2]))
}

// Given verve user disable, then the user is disabled and their sessions are revoked, with the system
// as actor of the event. Given verve user enable, then the user is active again.
func TestUserDisableAndEnable_ChangeTheStatus(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	mustCreate(t, deps, "--admin", "alice")
	mustCreate(t, deps, "bob")
	mustOpenSession(t, dataDir, "bob")

	res := run(t, deps, "user", "disable", "bob")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, "Disabled bob\n", res.stdout)

	bob := requireUser(t, dataDir, "bob", 0)
	require.Equal(t, store.UserDisabled, bob.Status)
	requireSystemEvent(t, dataDir, audit.TypeUserDisabled, bob)

	res = run(t, deps, "user", "enable", "bob")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, "Enabled bob\n", res.stdout)

	bob = requireUser(t, dataDir, "bob", 0)
	require.Equal(t, store.UserActive, bob.Status)
	requireSystemEvent(t, dataDir, audit.TypeUserEnabled, bob)
}

// Given the last active admin, then verve user disable fails as the API does and changes nothing.
// Given an unknown user, then the commands fail and say so.
func TestUserDisable_FollowsTheRulesOfTheAPI(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	mustCreate(t, deps, "--admin", "alice")
	mustOpenSession(t, dataDir, "alice")

	res := run(t, deps, "user", "disable", "alice")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: alice is the last active admin\n", res.stderr)

	alice := requireUser(t, dataDir, "alice", 1)
	require.Equal(t, store.UserActive, alice.Status)

	for _, args := range [][]string{{"user", "disable", "carol"}, {"user", "enable", "carol"}} {
		res = run(t, deps, args...)

		require.Equal(t, 1, res.code)
		require.Equal(t, "Error: no user \"carol\"\n", res.stderr)
	}

	res = runWithStdin(t, deps, alicePassword, "user", "reset-password", "--password-stdin", "carol")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: no user \"carol\"\n", res.stderr)
}

// Given verve user reset-password, then it reads the password like user create, from stdin or twice
// on the terminal, replaces the password of the user and revokes their sessions, with the system as
// actor of the event.
func TestUserResetPassword_ReplacesThePasswordAndRevokesTheSessions(t *testing.T) {
	t.Parallel()

	const newPassword = "a brand new passphrase"

	tests := []struct {
		name     string
		args     []string
		stdin    string
		terminal passwordFunc
		stderr   string
	}{
		{name: "stdin", args: []string{"--password-stdin"}, stdin: newPassword + "\n"},
		{name: "terminal", terminal: terminalAnswering(newPassword, newPassword), stderr: "Password: \nConfirm password: \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps, dataDir := userDeps(t)
			mustCreate(t, deps, "bob")
			mustOpenSession(t, dataDir, "bob")

			deps.ReadPassword = tt.terminal
			args := append(append([]string{"user", "reset-password"}, tt.args...), "bob")

			res := runWithStdin(t, deps, tt.stdin, args...)

			require.Equal(t, 0, res.code, res.stderr)
			require.Equal(t, "Reset the password of bob\n", res.stdout)
			require.Equal(t, tt.stderr, res.stderr)

			bob := requireUser(t, dataDir, "bob", 0)

			ok, err := auth.DefaultHasher().Verify(bob.PasswordHash, newPassword)
			require.NoError(t, err)
			require.True(t, ok, "the user must have the new password")

			requireSystemEvent(t, dataDir, audit.TypePasswordReset, bob)
		})
	}
}

// Given a password that breaks the policy or two different passwords, then verve user
// reset-password fails with what to change and keeps the password and the sessions.
func TestUserResetPassword_RefusesWhatBreaksTheRules(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	mustCreate(t, deps, "bob")
	mustOpenSession(t, dataDir, "bob")

	res := runWithStdin(t, deps, "short", "user", "reset-password", "--password-stdin", "bob")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: password must have at least 12 characters\n", res.stderr)

	deps.ReadPassword = terminalAnswering(alicePassword+"!", alicePassword+"?")
	res = run(t, deps, "user", "reset-password", "bob")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Password: \nConfirm password: \nError: passwords do not match\n", res.stderr)

	bob := requireUser(t, dataDir, "bob", 1)

	ok, err := auth.DefaultHasher().Verify(bob.PasswordHash, alicePassword)
	require.NoError(t, err)
	require.True(t, ok)
}

// Given serve holds the data directory lock, then the user commands still run, next to it.
func TestUserCommands_RunWhileTheLockIsHeld(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	mustCreate(t, deps, "--admin", "alice")
	mustCreate(t, deps, "bob")

	lock, err := app.AcquireLock(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	for _, args := range [][]string{{"user", "list"}, {"user", "disable", "bob"}, {"user", "enable", "bob"}} {
		res := run(t, deps, args...)
		require.Equal(t, 0, res.code, res.stderr)
	}

	res := runWithStdin(t, deps, alicePassword, "user", "reset-password", "--password-stdin", "bob")
	require.Equal(t, 0, res.code, res.stderr)
}
