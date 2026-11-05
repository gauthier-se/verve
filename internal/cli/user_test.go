package cli

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/app"
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
