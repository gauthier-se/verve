package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// PasswordChange is what a user sends to change their password.
type PasswordChange struct {
	UserID  int64
	Current string
	New     string
	// SessionID is the identifier of the session of the request, which the change keeps while it
	// revokes the other sessions of the user.
	SessionID string
}

// Passwords changes the passwords of users (FR-11). It shares the hashing slots and the rate
// limiter of the logins, so that a wrong current password counts as a failed login.
type Passwords struct {
	logins *Logins
	policy PasswordPolicy
}

// NewPasswords returns the passwords service, which verifies and hashes passwords as logins does,
// and checks new passwords against policy.
func NewPasswords(logins *Logins, policy PasswordPolicy) Passwords {
	return Passwords{logins: logins, policy: policy}
}

// Change verifies the current password of change and replaces it with the new one, through st, for
// the client of ctx. In one transaction, it stores the new hash, revokes every other session of the
// user and records the audit event.
//
// It returns the error of the password policy for a new password that breaks it,
// ErrInvalidCredentials for a wrong current password, which counts as a failed login of the
// username, a *RateLimitedError while the username or the client IP is delayed, and
// store.ErrNotFound for a user who no longer exists.
func (p Passwords) Change(ctx context.Context, st store.Store, change PasswordChange) error {
	if err := p.policy.Check(change.New); err != nil {
		return err
	}

	ip := audit.MetaFrom(ctx).IP

	username, err := p.attempt(ctx, st, change, ip)
	if errors.Is(err, errUserChanged) {
		// Such as by the hash upgrade of a concurrent login: the password is verified once more,
		// against the user as they are now.
		username, err = p.attempt(ctx, st, change, ip)
	}

	if errors.Is(err, errUserChanged) {
		p.logins.limiter.Fail(username, ip)

		err = ErrInvalidCredentials
	}

	var limited *RateLimitedError

	switch {
	case err == nil, errors.Is(err, ErrInvalidCredentials), errors.As(err, &limited):
		return err
	default:
		return fmt.Errorf("change password of user %d: %w", change.UserID, err)
	}
}

// attempt reads the user of change through st, verifies their current password, and stores the new
// one. It returns the username of the user, for the rate limiter, and errUserChanged when the user
// changed since they were read.
func (p Passwords) attempt(ctx context.Context, st store.Store, change PasswordChange, ip netip.Addr) (string, error) {
	user, err := st.Users().Get(ctx, change.UserID)
	if err != nil {
		return "", err
	}

	if err := p.logins.checkLimit(user.Username, ip); err != nil {
		return user.Username, err
	}

	hash, err := p.verifyAndHash(ctx, user, change, ip)
	if err != nil {
		return user.Username, err
	}

	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		current, err := tx.Users().Get(ctx, user.ID)
		if err != nil {
			return err
		}

		if current.PasswordHash != user.PasswordHash || current.Status != store.UserActive {
			return errUserChanged
		}

		if err := tx.Users().SetPasswordHash(ctx, user.ID, hash); err != nil {
			return err
		}

		if _, err := p.logins.sessions.RevokeOthers(ctx, tx, user.ID, change.SessionID); err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.PasswordChanged(user.ID))
	})

	return user.Username, err
}

// verifyAndHash verifies the current password of change against the hash of user and returns the
// hash of the new password, in a hashing slot. It returns ErrInvalidCredentials for a wrong current
// password or a user without a password, and counts the failure before it frees the slot, so that
// the attempts waiting for one see it.
func (p Passwords) verifyAndHash(ctx context.Context, user store.User, change PasswordChange, ip netip.Addr) (string, error) {
	l := p.logins

	select {
	case l.hashing <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("wait for a hashing slot: %w", context.Cause(ctx))
	}

	defer func() { <-l.hashing }()

	// The failures of other attempts may have reached the limit while this one waited.
	if err := l.checkLimit(user.Username, ip); err != nil {
		return "", err
	}

	if user.PasswordHash == "" {
		l.limiter.Fail(user.Username, ip)

		return "", ErrInvalidCredentials
	}

	ok, err := l.hasher.Verify(user.PasswordHash, change.Current)
	if err != nil {
		return "", fmt.Errorf("verify password of user %d: %w", user.ID, err)
	}

	if !ok {
		l.limiter.Fail(user.Username, ip)

		return "", ErrInvalidCredentials
	}

	return l.hasher.Hash(change.New), nil
}
