package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// fakeHash stands for a password hash: the store never looks into it.
const fakeHash = "$argon2id$hash"

// usersTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that CreatedAt
// is truncated to the millisecond and returned in UTC.
var usersTime = time.Date(2026, 10, 28, 10, 30, 15, 123_456_789, time.FixedZone("CET", 3600))

// caseVariants are logins that differ only by case. SQLite's NOCASE collation and lower() only fold
// ASCII, so the non-ASCII rows fail on an implementation that relies on them.
var caseVariants = []struct {
	name            string
	username        string
	usernameVariant string
	email           string
	emailVariant    string
}{
	{"ASCII", "alice", "ALICE", "alice@example.com", "ALICE@Example.COM"},
	{"Accent", "élodie", "Élodie", "élodie@example.com", "Élodie@example.com"},
	{"Cyrillic", "наташа", "НАТАША", "наташа@example.com", "НАТАША@example.com"},
	{"FinalSigma", "σας", "ΣΑΣ", "σας@example.com", "ΣΑΣ@example.com"},
}

func newUser(username, email string) store.NewUser {
	return store.NewUser{
		Username:     username,
		Email:        email,
		PasswordHash: "$argon2id$hash-of-" + username,
		Role:         store.RoleMember,
	}
}

func mustCreateUser(ctx context.Context, t *testing.T, users store.UsersRepository, user store.NewUser) store.User {
	t.Helper()

	got, err := users.Create(ctx, user)
	require.NoError(t, err)

	return got
}

func requireUserCount(ctx context.Context, t *testing.T, users store.UsersRepository, want int64) {
	t.Helper()

	got, err := users.Count(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// RunUsers runs the UsersRepository contract against the stores returned by newStore.
func RunUsers(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a created user, then Get returns it as active with the clock's now (millisecond
	// precision, UTC) as CreatedAt, its username in the case it was given, and no login yet.
	t.Run("CreateStoresEveryField", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		created := mustCreateUser(ctx, t, users, store.NewUser{
			Username:     "Alice",
			Email:        "Alice@Example.com",
			PasswordHash: fakeHash,
			Role:         store.RoleAdmin,
		})

		want := store.User{
			ID:           created.ID,
			Username:     "Alice",
			Email:        "Alice@Example.com",
			PasswordHash: fakeHash,
			Role:         store.RoleAdmin,
			Status:       store.UserActive,
			CreatedAt:    time.Date(2026, 10, 28, 9, 30, 15, 123_000_000, time.UTC),
		}
		require.Equal(t, want, created)

		got, err := users.Get(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// Given a user with no email and no password hash, as with the external logins of M5, then both
	// stay empty.
	t.Run("EmailAndPasswordHashAreOptional", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		created := mustCreateUser(ctx, t, users, store.NewUser{Username: "alice", Role: store.RoleMember})

		got, err := users.Get(ctx, created.ID)
		require.NoError(t, err)
		require.Empty(t, got.Email)
		require.Empty(t, got.PasswordHash)
	})

	// Given a username that differs only by case from an existing one, then store.ErrConflict, and
	// no second user exists.
	t.Run("UsernameDifferingOnlyByCaseConflicts", func(t *testing.T) {
		t.Parallel()

		for _, tc := range caseVariants {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				users := newStore(t, clock.NewFake(usersTime)).Users()
				ctx := t.Context()

				mustCreateUser(ctx, t, users, newUser(tc.username, ""))

				_, err := users.Create(ctx, newUser(tc.usernameVariant, ""))
				require.ErrorIs(t, err, store.ErrConflict)
				requireUserCount(ctx, t, users, 1)
			})
		}
	})

	// Given an email that differs only by case from the email of another user, then
	// store.ErrConflict, and no second user exists.
	t.Run("EmailDifferingOnlyByCaseConflicts", func(t *testing.T) {
		t.Parallel()

		for _, tc := range caseVariants {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				users := newStore(t, clock.NewFake(usersTime)).Users()
				ctx := t.Context()

				mustCreateUser(ctx, t, users, newUser("first", tc.email))

				_, err := users.Create(ctx, newUser("second", tc.emailVariant))
				require.ErrorIs(t, err, store.ErrConflict)
				requireUserCount(ctx, t, users, 1)
			})
		}
	})

	// Given several users without an email, then they do not conflict.
	t.Run("UsersWithoutEmailDoNotConflict", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		mustCreateUser(ctx, t, users, newUser("first", ""))
		mustCreateUser(ctx, t, users, newUser("second", ""))

		requireUserCount(ctx, t, users, 2)
	})

	// Given an email without @, then store.ErrInvalid, and no user exists.
	t.Run("EmailWithoutAtSignIsInvalid", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		_, err := users.Create(ctx, newUser("alice", "alice.example.com"))
		require.ErrorIs(t, err, store.ErrInvalid)
		requireUserCount(ctx, t, users, 0)
	})

	// Given a username with @, then store.ErrInvalid: GetByLogin looks a login up in usernames and
	// emails at once, so this is what keeps a login from matching two users.
	t.Run("UsernameWithAtSignIsInvalid", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		_, err := users.Create(ctx, newUser("alice@example.com", ""))
		require.ErrorIs(t, err, store.ErrInvalid)
		requireUserCount(ctx, t, users, 0)
	})

	// Given Get on a missing id, then the error satisfies errors.Is(err, store.ErrNotFound).
	t.Run("GetOnMissingIDReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()

		_, err := users.Get(t.Context(), 42)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given a login in any case, then GetByLogin finds the user by username and by email.
	t.Run("GetByLoginFindsTheUserByUsernameOrEmailIgnoringCase", func(t *testing.T) {
		t.Parallel()

		for _, tc := range caseVariants {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				users := newStore(t, clock.NewFake(usersTime)).Users()
				ctx := t.Context()

				mustCreateUser(ctx, t, users, newUser("other", "other@example.com"))
				created := mustCreateUser(ctx, t, users, newUser(tc.username, tc.email))

				for _, login := range []string{tc.username, tc.usernameVariant, tc.email, tc.emailVariant} {
					got, err := users.GetByLogin(ctx, login)
					require.NoError(t, err, login)
					require.Equal(t, created.ID, got.ID, login)
				}
			})
		}
	})

	// Given a login that no user has, then store.ErrNotFound. An empty login matches no user, not
	// even one without an email.
	t.Run("GetByLoginOnUnknownLoginReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		mustCreateUser(ctx, t, users, newUser("alice", ""))

		for _, login := range []string{"bob", "bob@example.com", ""} {
			_, err := users.GetByLogin(ctx, login)
			require.ErrorIs(t, err, store.ErrNotFound, login)
		}
	})

	// Given users, then Count returns their number, starting from 0 on a new store.
	t.Run("CountReturnsTheNumberOfUsers", func(t *testing.T) {
		t.Parallel()

		users := newStore(t, clock.NewFake(usersTime)).Users()
		ctx := t.Context()

		requireUserCount(ctx, t, users, 0)

		mustCreateUser(ctx, t, users, newUser("first", ""))
		mustCreateUser(ctx, t, users, newUser("second", ""))

		requireUserCount(ctx, t, users, 2)
	})

	// Given a user created through the repositories of WithTx, when the transaction rolls back, then
	// no user exists.
	t.Run("RolledBackTransactionLeavesNoUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(usersTime))
		ctx := t.Context()
		errRollback := errors.New("roll back")

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustCreateUser(ctx, t, tx.Users(), newUser("alice", ""))
			requireUserCount(ctx, t, tx.Users(), 1)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		requireUserCount(ctx, t, s.Users(), 0)
	})

	// Given a conflict inside WithTx that fn handles, then the transaction stays usable and commits
	// the writes of fn.
	t.Run("ConflictInsideATransactionLeavesItUsable", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(usersTime))
		ctx := t.Context()

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustCreateUser(ctx, t, tx.Users(), newUser("alice", ""))

			_, err := tx.Users().Create(ctx, newUser("ALICE", ""))
			require.ErrorIs(t, err, store.ErrConflict)

			mustCreateUser(ctx, t, tx.Users(), newUser("bob", ""))

			return nil
		})
		require.NoError(t, err)

		requireUserCount(ctx, t, s.Users(), 2)
	})
}
