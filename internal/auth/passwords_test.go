package auth

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// newPassword is the password that the tests change to.
const newPassword = "a brand new passphrase" //nolint:gosec // G101: a fake password of the tests

// editedGets wraps a real store, so that its first lookup of a user by id, outside a transaction,
// returns the user changed by edit: as it was before another request changed it. The next lookups
// return the stored user.
type editedGets struct {
	store.Store

	edit   func(*store.User)
	edited atomic.Bool
}

func (s *editedGets) Users() store.UsersRepository {
	return editedGetUsers{UsersRepository: s.Store.Users(), gets: s}
}

type editedGetUsers struct {
	store.UsersRepository

	gets *editedGets
}

func (u editedGetUsers) Get(ctx context.Context, id int64) (store.User, error) {
	user, err := u.UsersRepository.Get(ctx, id)
	if err == nil && u.gets.edited.CompareAndSwap(false, true) {
		u.gets.edit(&user)
	}

	return user, err
}

// changePassword changes the password of user from alicePassword to newPassword through st, from
// the phone.
func (lt loginTest) changePassword(ctx context.Context, st store.Store, user store.User) error {
	ctx = audit.WithMeta(ctx, audit.Meta{IP: phoneIP, UserAgent: "Mozilla/5.0 (iPhone)"})

	return NewPasswords(lt.logins, NewPasswordPolicy()).Change(ctx, st, PasswordChange{
		UserID:  user.ID,
		Current: alicePassword,
		New:     newPassword,
	})
}

// Given a password that changed between its verification and the change, then the change verifies
// the current password again against the new hash, fails and keeps the hash: the verified password
// is no longer the one of the user.
func TestPasswords_PasswordChangedSinceItsVerificationFails(t *testing.T) {
	t.Parallel()

	lt := newLoginTest(t)
	alice := lt.mustCreateUser(t, "alice", currentHash("a new password, changed meanwhile"))
	st := &editedGets{Store: lt.store, edit: func(u *store.User) { u.PasswordHash = currentHash(alicePassword) }}

	require.ErrorIs(t, lt.changePassword(t.Context(), st, alice), ErrInvalidCredentials)

	stored, err := lt.store.Users().Get(t.Context(), alice.ID)
	require.NoError(t, err)
	require.Equal(t, currentHash("a new password, changed meanwhile"), stored.PasswordHash)

	require.Equal(t, []string{currentHash(alicePassword), currentHash("a new password, changed meanwhile")},
		lt.hasher.recorded().verifiedHashes)
}

// Given a hash that a concurrent login upgraded between the verification and the change, then the
// change verifies the current password again against the upgraded hash and succeeds.
func TestPasswords_HashUpgradedByAConcurrentLoginIsVerifiedAgain(t *testing.T) {
	t.Parallel()

	lt := newLoginTest(t)
	alice := lt.mustCreateUser(t, "alice", currentHash(alicePassword))
	st := &editedGets{Store: lt.store, edit: func(u *store.User) { u.PasswordHash = aliceOutdatedHash }}

	require.NoError(t, lt.changePassword(t.Context(), st, alice))

	stored, err := lt.store.Users().Get(t.Context(), alice.ID)
	require.NoError(t, err)
	require.Equal(t, currentHash(newPassword), stored.PasswordHash)

	require.Equal(t, []string{aliceOutdatedHash, currentHash(alicePassword)}, lt.hasher.recorded().verifiedHashes)
}

// Given a user without a password, such as one of an external identity, then the change fails as a
// wrong current password, without verifying a hash, and counts in the login rate limit.
func TestPasswords_UserWithoutPasswordFails(t *testing.T) {
	t.Parallel()

	lt := newLoginTest(t)
	alice := lt.mustCreateUser(t, "alice", "")

	for range 5 {
		require.ErrorIs(t, lt.changePassword(t.Context(), lt.store, alice), ErrInvalidCredentials)
	}

	var limited *RateLimitedError
	require.ErrorAs(t, lt.changePassword(t.Context(), lt.store, alice), &limited)

	require.Empty(t, lt.hasher.recorded().verifiedHashes)

	stored, err := lt.store.Users().Get(t.Context(), alice.ID)
	require.NoError(t, err)
	require.Empty(t, stored.PasswordHash)
}
