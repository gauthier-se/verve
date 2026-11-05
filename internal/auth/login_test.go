package auth

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// The passwords of the login tests. The fake hasher hashes them as currentHash and outdatedHash do.
const (
	alicePassword = "correct horse battery staple"
	wrongPassword = "Tr0ub4dor&3" //nolint:gosec // G101: a fake password of the tests
)

// currentHash and outdatedHash return the hash of password that fakeHasher writes with its current
// parameters, and one with former parameters.
func currentHash(password string) string  { return "v2:" + password }
func outdatedHash(password string) string { return "v1:" + password }

// fakeHasher stands for the argon2id hasher: its hashes hold the password after the version of their
// parameters. It counts its calls, so that the tests prove the timing of a login by the hashes it
// computes rather than with a stopwatch.
type fakeHasher struct {
	// block holds, by password, a channel that the verification of that password waits for until
	// it is closed. It is set before the logins start.
	block map[string]chan struct{}

	mu sync.Mutex
	// verified holds the hash of each verification, in order.
	verified []string
	// passwords holds the password of each verification, in order.
	passwords []string
	// verifying is the number of verifications running.
	verifying int
	// hashes is the number of hashes computed.
	hashes int
}

func (f *fakeHasher) Hash(password string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.hashes++

	return currentHash(password)
}

func (f *fakeHasher) Verify(hash, password string) (bool, error) {
	f.mu.Lock()
	f.verified = append(f.verified, hash)
	f.passwords = append(f.passwords, password)
	f.verifying++
	f.mu.Unlock()

	if gate, ok := f.block[password]; ok {
		<-gate
	}

	f.mu.Lock()
	f.verifying--
	f.mu.Unlock()

	_, stored, ok := strings.Cut(hash, ":")

	return ok && stored == password, nil
}

func (f *fakeHasher) NeedsRehash(hash string) (bool, error) {
	return !strings.HasPrefix(hash, "v2:"), nil
}

// calls returns the hashes verified, the passwords verified, the verifications running and the
// hashes computed.
func (f *fakeHasher) calls() (verified, passwords []string, verifying, hashes int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.verified...), append([]string(nil), f.passwords...), f.verifying, f.hashes
}

// loginTest holds a store on a migrated database, the fake clock, the fake hasher, the limiter and
// the logins service on them.
type loginTest struct {
	store    store.Store
	clock    *clock.Fake
	hasher   *fakeHasher
	limiter  *LoginLimiter
	sessions Sessions
	logins   *Logins
}

// newLoginTest returns a loginTest on a fresh temporary directory, at sessionsTestNow, whose hasher
// blocks the verification of the passwords of block.
func newLoginTest(t *testing.T, block ...string) loginTest {
	t.Helper()

	s, sessions, clk := newTestSessions(t)
	hasher := &fakeHasher{block: map[string]chan struct{}{}}

	for _, password := range block {
		hasher.block[password] = make(chan struct{})
	}

	limiter := NewLoginLimiter(clk)

	return loginTest{
		store:    s,
		clock:    clk,
		hasher:   hasher,
		limiter:  limiter,
		sessions: sessions,
		logins:   NewLogins(sessions, hasher, limiter),
	}
}

// mustCreateUser stores an active member named username, with an email at example.com and the
// password hash hash.
func (lt loginTest) mustCreateUser(t *testing.T, username, hash string) store.User {
	t.Helper()

	user, err := lt.store.Users().Create(t.Context(), store.NewUser{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: hash,
		Role:         store.RoleMember,
	})
	require.NoError(t, err)

	return user
}

// login logs in as login with password, from the client at ip.
func (lt loginTest) login(ctx context.Context, ip netip.Addr, login, password string) (LoggedIn, error) {
	ctx = audit.WithMeta(ctx, audit.Meta{IP: ip, UserAgent: "Mozilla/5.0 (iPhone)"})

	return lt.logins.Login(ctx, lt.store, Credentials{Login: login, Password: password})
}

// editedLogins wraps a real store, so that GetByLogin returns its user changed by edit. No
// repository changes the status of a user yet.
type editedLogins struct {
	store.Store

	edit func(*store.User)
}

func (s editedLogins) Users() store.UsersRepository {
	return editedUsers{UsersRepository: s.Store.Users(), edit: s.edit}
}

type editedUsers struct {
	store.UsersRepository

	edit func(*store.User)
}

func (u editedUsers) GetByLogin(ctx context.Context, login string) (store.User, error) {
	user, err := u.UsersRepository.GetByLogin(ctx, login)
	if err == nil {
		u.edit(&user)
	}

	return user, err
}

// Given valid credentials, by username or by email in any case, then Login opens a session of the
// user from the client of the context, records the time of the login, and returns the user and the
// session identifier.
func TestLogins_ValidCredentialsOpenASession(t *testing.T) {
	t.Parallel()

	for _, login := range []string{"alice", "ALICE", "alice@example.com", "Alice@Example.com"} {
		t.Run(login, func(t *testing.T) {
			t.Parallel()

			lt := newLoginTest(t)
			alice := lt.mustCreateUser(t, "alice", currentHash(alicePassword))
			lt.clock.Advance(time.Hour)

			got, err := lt.login(t.Context(), phoneIP, login, alicePassword)
			require.NoError(t, err)

			require.Equal(t, alice.ID, got.User.ID)
			require.Equal(t, sessionsTestNow.Add(time.Hour), got.User.LastLoginAt)

			found, err := lt.sessions.Lookup(t.Context(), lt.store, got.SessionID)
			require.NoError(t, err)
			require.Equal(t, got.Session, found.Session)
			require.Equal(t, store.Session{
				PublicID:   got.Session.PublicID,
				UserID:     alice.ID,
				Method:     store.LoginPassword,
				CreatedAt:  sessionsTestNow.Add(time.Hour),
				LastSeenAt: sessionsTestNow.Add(time.Hour),
				ExpiresAt:  sessionsTestNow.Add(time.Hour + 30*day),
				ClientIP:   "203.0.113.7",
				UserAgent:  "Mozilla/5.0 (iPhone)",
			}, got.Session)
		})
	}
}

// Given a wrong password, an unknown login, a disabled account, or a user without a password, then
// Login returns ErrInvalidCredentials alone, so that callers cannot tell them apart, and records
// no login.
func TestLogins_FailuresReturnTheSameError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		login    string
		password string
		// edit changes the user that the store finds, or is nil.
		edit func(*store.User)
	}{
		{name: "WrongPassword", login: "alice", password: wrongPassword},
		{name: "UnknownLogin", login: "bob", password: alicePassword},
		{name: "DisabledAccount", login: "alice", password: alicePassword, edit: func(u *store.User) {
			u.Status = store.UserDisabled
		}},
		{name: "DeletingAccount", login: "alice", password: alicePassword, edit: func(u *store.User) {
			u.Status = store.UserDeleting
		}},
		{name: "NoPassword", login: "alice", password: alicePassword, edit: func(u *store.User) {
			u.PasswordHash = ""
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lt := newLoginTest(t)
			alice := lt.mustCreateUser(t, "alice", currentHash(alicePassword))

			if tt.edit != nil {
				lt.store = editedLogins{Store: lt.store, edit: tt.edit}
			}

			got, err := lt.login(t.Context(), phoneIP, tt.login, tt.password)
			require.ErrorIs(t, err, ErrInvalidCredentials)
			require.EqualError(t, err, ErrInvalidCredentials.Error())
			require.Equal(t, LoggedIn{}, got)

			stored, err := lt.store.Users().Get(t.Context(), alice.ID)
			require.NoError(t, err)
			require.True(t, stored.LastLoginAt.IsZero())
		})
	}
}

// Given an unknown login, or a user without a password, then Login still verifies the password
// once, against a dummy hash with the current parameters, as it verifies a known one against its
// hash: the timing of a login does not reveal whether its user exists.
func TestLogins_UnknownLoginVerifiesADummyHash(t *testing.T) {
	t.Parallel()

	lt := newLoginTest(t)
	lt.mustCreateUser(t, "alice", currentHash(alicePassword))
	lt.mustCreateUser(t, "carol", "")

	_, err := lt.login(t.Context(), phoneIP, "alice", wrongPassword)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	verified, _, _, _ := lt.hasher.calls()
	require.Equal(t, []string{currentHash(alicePassword)}, verified)

	for _, login := range []string{"bob", "carol", "dave@example.com"} {
		_, err := lt.login(t.Context(), phoneIP, login, wrongPassword)
		require.ErrorIs(t, err, ErrInvalidCredentials, login)
	}

	verified, _, _, _ = lt.hasher.calls()
	require.Len(t, verified, 4)

	dummy := verified[1]
	require.Equal(t, []string{dummy, dummy, dummy}, verified[1:])
	require.NotEqual(t, currentHash(alicePassword), dummy)

	outdated, err := lt.hasher.NeedsRehash(dummy)
	require.NoError(t, err)
	require.False(t, outdated)
}

// Given a hash with the former parameters, then a successful login replaces it with a hash of the
// current ones. A hash with the current parameters, or a failed login, leaves the hash as it is.
func TestLogins_SuccessUpgradesAnOutdatedHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hash     string
		password string
		want     string
		// hashes is the number of hashes that the login computes.
		hashes int
	}{
		{name: "Outdated", hash: outdatedHash(alicePassword), password: alicePassword, want: currentHash(alicePassword), hashes: 1},
		{name: "Current", hash: currentHash(alicePassword), password: alicePassword, want: currentHash(alicePassword)},
		{name: "OutdatedWrongPassword", hash: outdatedHash(alicePassword), password: wrongPassword, want: outdatedHash(alicePassword)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lt := newLoginTest(t)
			alice := lt.mustCreateUser(t, "alice", tt.hash)

			_, _ = lt.login(t.Context(), phoneIP, "alice", tt.password)

			stored, err := lt.store.Users().Get(t.Context(), alice.ID)
			require.NoError(t, err)
			require.Equal(t, tt.want, stored.PasswordHash)

			_, _, _, hashes := lt.hasher.calls()
			require.Equal(t, tt.hashes, hashes)
		})
	}
}

// Given a password that changed between its verification and the opening of the session, then the
// login fails and opens no session: the verified password is no longer the one of the user.
func TestLogins_PasswordChangedSinceItsVerificationFails(t *testing.T) {
	t.Parallel()

	lt := newLoginTest(t)
	alice := lt.mustCreateUser(t, "alice", currentHash("a new password, changed meanwhile"))
	lt.store = editedLogins{Store: lt.store, edit: func(u *store.User) {
		u.PasswordHash = currentHash(alicePassword)
	}}

	_, err := lt.login(t.Context(), phoneIP, "alice", alicePassword)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	stored, err := lt.store.Users().Get(t.Context(), alice.ID)
	require.NoError(t, err)
	require.True(t, stored.LastLoginAt.IsZero())
	require.Equal(t, currentHash("a new password, changed meanwhile"), stored.PasswordHash)
}

// Given more concurrent logins than the hashing slots, then the others wait for a slot before
// they verify their password, so that argon2id cannot exhaust the memory of a Raspberry Pi.
func TestLogins_ConcurrentLoginsBeyondTheSlotsWait(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		passwords := []string{"first password", "second password", "third password"}
		lt := newLoginTest(t, passwords...)
		lt.mustCreateUser(t, "alice", currentHash(alicePassword))

		var wg sync.WaitGroup

		errs := make(chan error, len(passwords))
		for _, password := range passwords {
			wg.Go(func() {
				_, err := lt.login(t.Context(), phoneIP, "alice", password)
				errs <- err
			})
		}

		synctest.Wait()

		_, entered, verifying, _ := lt.hasher.calls()
		require.Len(t, entered, hashingSlots)
		require.Equal(t, hashingSlots, verifying)

		close(lt.hasher.block[entered[0]])
		synctest.Wait()

		_, entered, verifying, _ = lt.hasher.calls()
		require.ElementsMatch(t, passwords, entered)
		require.Equal(t, hashingSlots, verifying)

		for _, password := range entered[1:] {
			close(lt.hasher.block[password])
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.ErrorIs(t, err, ErrInvalidCredentials)
		}
	})
}

// Given an attempt that waited for a hashing slot while the failures of its login reached the
// limit, then it is refused once it has the slot, without verifying its password: concurrent
// attempts cannot get past the limit.
func TestLogins_AttemptThatWaitedForASlotIsCheckedAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lt := newLoginTest(t, "fifth guess", "guess of another login", "sixth guess")
		lt.mustCreateUser(t, "alice", currentHash(alicePassword))
		failTimes(lt.limiter, loginFailureLimit-1, "alice", phoneIP)

		var wg sync.WaitGroup

		attempt := func(ip netip.Addr, login, password string) <-chan error {
			result := make(chan error, 1)

			wg.Go(func() {
				_, err := lt.login(t.Context(), ip, login, password)
				result <- err
			})
			synctest.Wait()

			return result
		}

		fifth := attempt(phoneIP, "alice", "fifth guess")
		other := attempt(laptopIP, "bob", "guess of another login")
		sixth := attempt(phoneIP, "alice", "sixth guess")

		close(lt.hasher.block["fifth guess"])
		require.ErrorIs(t, <-fifth, ErrInvalidCredentials)

		var limited *RateLimitedError
		require.ErrorAs(t, <-sixth, &limited)
		require.Equal(t, time.Second, limited.RetryAfter)

		close(lt.hasher.block["guess of another login"])
		require.ErrorIs(t, <-other, ErrInvalidCredentials)

		wg.Wait()

		_, passwords, _, _ := lt.hasher.calls()
		require.Equal(t, []string{"fifth guess", "guess of another login"}, passwords)
	})
}

// Given a login whose attempts are delayed, then Login returns a RateLimitedError with the delay,
// from any client IP, without verifying the password, even a right one.
func TestLogins_DelayedAttemptIsRateLimited(t *testing.T) {
	t.Parallel()

	lt := newLoginTest(t)
	lt.mustCreateUser(t, "alice", currentHash(alicePassword))

	for range loginFailureLimit {
		_, err := lt.login(t.Context(), phoneIP, "alice", wrongPassword)
		require.ErrorIs(t, err, ErrInvalidCredentials)
	}

	_, err := lt.login(t.Context(), laptopIP, "alice", alicePassword)

	var limited *RateLimitedError
	require.ErrorAs(t, err, &limited)
	require.Equal(t, time.Second, limited.RetryAfter)
	require.NotErrorIs(t, err, ErrInvalidCredentials)

	verified, _, _, _ := lt.hasher.calls()
	require.Len(t, verified, loginFailureLimit)
}
