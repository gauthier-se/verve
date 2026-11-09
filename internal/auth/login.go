package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// hashingSlots is the number of passwords that logins hash at once. An argon2id hash of the
// default parameters takes 19 MiB: the other logins wait for a slot, so that a burst of them cannot
// exhaust the memory of a Raspberry Pi.
const hashingSlots = 2

// dummyPasswordLength is the length of the random password of the dummy hash, in bytes.
const dummyPasswordLength = 32

// ErrInvalidCredentials is returned alike for a wrong password, an unknown login and a user who is
// not active or has no password, so that callers cannot tell them apart.
var ErrInvalidCredentials = errors.New("invalid credentials")

// RateLimitedError is returned for a login attempt made before the delay of the limiter passed.
type RateLimitedError struct {
	// RetryAfter is how long the client must wait before its next attempt.
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many failed logins, retry in %s", e.RetryAfter)
}

// PasswordHasher hashes passwords and verifies them. Hasher is the one of production.
type PasswordHasher interface {
	// Hash returns the hash of password with the current parameters.
	Hash(password string) string
	// Verify reports whether password is the one of hash.
	Verify(hash, password string) (bool, error)
	// NeedsRehash reports whether hash has parameters below the current ones.
	NeedsRehash(hash string) (bool, error)
}

// Credentials is what a client sends to log in.
type Credentials struct {
	// Login is a username or an email, in any case.
	Login    string
	Password string
	// SessionID is the identifier of the session that the client already has, which the login
	// replaces, or empty.
	SessionID string
}

// LoggedIn is the result of a successful login.
type LoggedIn struct {
	User store.User
	// SessionID is the identifier of the new session, returned once: the store only holds its hash.
	SessionID string
	Session   store.Session
}

// Logins logs users in with their password (FR-1, FR-3, FR-4, FR-9).
type Logins struct {
	sessions Sessions
	hasher   PasswordHasher
	limiter  *LoginLimiter
	// hashing holds a token for each password being hashed, at most hashingSlots.
	hashing chan struct{}
	// dummyHash returns the hash verified in place of the one of an unknown login, computed on its
	// first call with the current parameters.
	dummyHash func() string
}

// NewLogins returns the logins service, which opens sessions with sessions, hashes passwords with
// hasher and delays attempts with limiter.
func NewLogins(sessions Sessions, hasher PasswordHasher, limiter *LoginLimiter) *Logins {
	return &Logins{
		sessions: sessions,
		hasher:   hasher,
		limiter:  limiter,
		hashing:  make(chan struct{}, hashingSlots),
		dummyHash: sync.OnceValue(func() string {
			return hasher.Hash(randomText(dummyPasswordLength))
		}),
	}
}

// Login logs the client of ctx, as audit.MetaFrom describes it, in as creds.Login with
// creds.Password, through st. On success, it upgrades an outdated password hash, records the time
// of the login, replaces the session creds.SessionID with a new one, and records the audit event,
// in one transaction.
//
// It returns ErrInvalidCredentials for a failed login, which it records, and a *RateLimitedError
// while the login or the client IP is delayed. Every attempt that reaches the password verifies
// one, against a dummy hash for an unknown login, so that its timing does not reveal whether the
// login exists.
func (l *Logins) Login(ctx context.Context, st store.Store, creds Credentials) (LoggedIn, error) {
	client := audit.MetaFrom(ctx)

	if err := l.checkLimit(creds.Login, client.IP); err != nil {
		return LoggedIn{}, err
	}

	loggedIn, userID, err := l.attempt(ctx, st, creds, client)
	if errors.Is(err, errUserChanged) {
		// Such as by the hash upgrade of a concurrent login of the user, or by a password change:
		// the password is verified once more, against the user as they are now.
		loggedIn, userID, err = l.attempt(ctx, st, creds, client)
	}

	if errors.Is(err, errUserChanged) {
		l.limiter.Fail(creds.Login, client.IP)

		err = ErrInvalidCredentials
	}

	if errors.Is(err, ErrInvalidCredentials) {
		return LoggedIn{}, l.recordFailure(ctx, st, creds.Login, userID)
	}

	if err != nil {
		return LoggedIn{}, fmt.Errorf("log in: %w", err)
	}

	l.limiter.Reset(creds.Login)

	return loggedIn, nil
}

// errUserChanged is returned when the password hash or the status of the user changed between the
// verification of the password and the opening of the session.
var errUserChanged = errors.New("user changed since the password was verified")

// attempt looks the user of creds up through st, verifies the password and opens the session of
// the client. It returns the id of the user of the login, or 0 when no user has it, and
// ErrInvalidCredentials for a failure, which the limiter counted, or errUserChanged when the user
// changed since they were read.
func (l *Logins) attempt(ctx context.Context, st store.Store, creds Credentials, client audit.Meta) (LoggedIn, int64, error) {
	user, err := st.Users().GetByLogin(ctx, creds.Login)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return LoggedIn{}, 0, err
	}

	rehash, err := l.checkPassword(ctx, creds, client.IP, user)
	if err != nil {
		return LoggedIn{}, user.ID, err
	}

	loggedIn, err := l.openSession(ctx, st, user, rehash, creds.SessionID, client)

	return loggedIn, user.ID, err
}

// checkLimit returns a *RateLimitedError while the attempts of login or ip are delayed.
func (l *Logins) checkLimit(login string, ip netip.Addr) error {
	if wait := l.limiter.Wait(login, ip); wait > 0 {
		return &RateLimitedError{RetryAfter: wait}
	}

	return nil
}

// checkPassword verifies the password of creds against the hash of user, or against the dummy hash
// when user is unknown or has no password, in a hashing slot. It returns the hash of the password
// with the current parameters when the one of user is outdated, or empty.
//
// It returns ErrInvalidCredentials for a wrong password or a user who is not active, and counts the
// failure before it frees the slot, so that the attempts waiting for one see it.
func (l *Logins) checkPassword(ctx context.Context, creds Credentials, ip netip.Addr, user store.User) (string, error) {
	var rehash string

	err := l.inHashingSlot(ctx, creds.Login, ip, func() error {
		var err error

		rehash, err = l.verifyInSlot(creds, ip, user)

		return err
	})

	return rehash, err
}

// inHashingSlot runs fn in a hashing slot, once it checked again that login and ip are not
// delayed: the failures of other attempts may have reached the limit while it waited for the slot.
// fn counts its failures before the slot is freed, so that the attempts waiting for one see them.
func (l *Logins) inHashingSlot(ctx context.Context, login string, ip netip.Addr, fn func() error) error {
	select {
	case l.hashing <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait for a hashing slot: %w", context.Cause(ctx))
	}

	defer func() { <-l.hashing }()

	if err := l.checkLimit(login, ip); err != nil {
		return err
	}

	return fn()
}

// verifyInSlot is the part of checkPassword that runs in a hashing slot.
func (l *Logins) verifyInSlot(creds Credentials, ip netip.Addr, user store.User) (string, error) {
	// The first login since the start computes the dummy hash, whether or not its user exists.
	dummy := l.dummyHash()

	hash := user.PasswordHash
	if hash == "" {
		hash = dummy
	}

	ok, err := l.hasher.Verify(hash, creds.Password)
	if err != nil {
		return "", fmt.Errorf("verify password of user %d: %w", user.ID, err)
	}

	if !ok || user.PasswordHash == "" || user.Status != store.UserActive {
		l.limiter.Fail(creds.Login, ip)

		return "", ErrInvalidCredentials
	}

	outdated, err := l.hasher.NeedsRehash(hash)
	if err != nil {
		return "", fmt.Errorf("check password hash of user %d: %w", user.ID, err)
	}

	if !outdated {
		return "", nil
	}

	return l.hasher.Hash(creds.Password), nil
}

// openSession opens the session of user from client, through st, in one transaction: it stores
// rehash as the password hash unless empty, records the time of the login, revokes the session
// replaced unless empty, creates the new session and records the audit event. It returns
// errUserChanged when user was deleted, or their password hash or status changed, since they were
// read: the verified password may no longer be the one of user.
func (l *Logins) openSession(ctx context.Context, st store.Store, user store.User, rehash, replaced string, client audit.Meta) (LoggedIn, error) {
	var loggedIn LoggedIn

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		current, err := tx.Users().Get(ctx, user.ID)
		if errors.Is(err, store.ErrNotFound) {
			return errUserChanged
		}

		if err != nil {
			return err
		}

		if changedSince(current, user) {
			return errUserChanged
		}

		if rehash != "" {
			if err := tx.Users().SetPasswordHash(ctx, user.ID, rehash); err != nil {
				return err
			}
		}

		recorded, err := tx.Users().RecordLogin(ctx, user.ID)
		if err != nil {
			return err
		}

		if replaced != "" {
			if err := l.sessions.Revoke(ctx, tx, replaced); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}

		id, session, err := l.sessions.Create(ctx, tx, NewSession{
			UserID:    user.ID,
			Method:    store.LoginPassword,
			ClientIP:  ipText(client.IP),
			UserAgent: client.UserAgent,
		})
		if err != nil {
			return err
		}

		if err := audit.Record(ctx, tx.AuditEvents(), audit.LoginSucceeded(user.ID, store.LoginPassword)); err != nil {
			return err
		}

		loggedIn = LoggedIn{User: recorded, SessionID: id, Session: session}

		return nil
	})

	return loggedIn, err
}

// changedSince reports whether current, the user as the store holds them now, was deleted, or had
// their password hash or status changed, since they were read as user: a password verified against
// user may no longer be theirs.
func changedSince(current, user store.User) bool {
	return current.PasswordHash != user.PasswordHash || current.Status != store.UserActive
}

// recordFailure records the failed login of login through st, about the user of userID who has
// that login, or about no one for 0, and returns ErrInvalidCredentials, or the error of the record.
func (l *Logins) recordFailure(ctx context.Context, st store.Store, login string, userID int64) error {
	if err := audit.Record(ctx, st.AuditEvents(), audit.LoginFailed(login, userID)); err != nil {
		return fmt.Errorf("log in: %w", err)
	}

	return ErrInvalidCredentials
}

// ipText returns ip as text, or empty when it is unknown.
func ipText(ip netip.Addr) string {
	if !ip.IsValid() {
		return ""
	}

	return ip.String()
}
