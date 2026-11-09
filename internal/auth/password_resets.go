package auth

import (
	"context"
	"errors"
	"time"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// resetLinkLifetime is how long a password reset link stays valid (FR-6 of accounts and
// administration).
const resetLinkLifetime = 24 * time.Hour

// ErrResetLinkInvalid is returned for a token that no unused, unexpired reset link has. Its message
// is the stable code that the API answers with.
var ErrResetLinkInvalid = errors.New("reset_link_invalid")

// CreatedResetLink is a stored reset link and its token, returned once: the store only holds its
// hash.
type CreatedResetLink struct {
	Link  store.Invitation
	Token string
}

// PasswordResets replaces passwords through a single-use link that an admin creates and gives to
// the user, who chooses the new password: the admin never sets nor sees it (FR-6 of accounts and
// administration). The links are invitations of kind store.InvitationReset.
type PasswordResets struct {
	clk      clock.Clock
	accounts Accounts
	sessions Sessions
}

// NewPasswordResets returns the password reset service, which takes the current time from clk,
// checks and hashes passwords with accounts and revokes sessions with sessions.
func NewPasswordResets(clk clock.Clock, accounts Accounts, sessions Sessions) PasswordResets {
	return PasswordResets{clk: clk, accounts: accounts, sessions: sessions}
}

// Create stores a reset link valid 24 hours for the user of userID through st, created by the admin
// of adminID, and returns it with its token. In one transaction, it deletes the unused reset links
// of the user, so that only the new one works, and records the audit event with the client of ctx.
// It returns ErrUserDeleting for an account being deleted and store.ErrNotFound for an unknown
// user.
func (p PasswordResets) Create(ctx context.Context, st store.Store, adminID, userID int64) (CreatedResetLink, error) {
	token := randomText(invitationTokenLength)

	var created store.Invitation

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if _, err := changeableUser(ctx, tx.Users(), userID); err != nil {
			return err
		}

		if err := tx.Invitations().DeleteUnusedResets(ctx, userID); err != nil {
			return err
		}

		var err error

		created, err = tx.Invitations().Create(ctx, store.NewInvitation{
			Kind:      store.InvitationReset,
			TokenHash: hashLinkToken(token),
			UserID:    userID,
			ExpiresAt: p.clk.Now().Add(resetLinkLifetime),
			CreatedBy: adminID,
		})
		if err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.PasswordResetLinkCreated(adminID, userID))
	})
	if err != nil {
		return CreatedResetLink{}, err
	}

	return CreatedResetLink{Link: created, Token: token}, nil
}

// Reset replaces the password of the user of the reset link of token with password through st. In
// one transaction, it stores the hash, marks the link used, revokes every session of the user and
// records the audit event with the client of ctx. It opens no session: the user logs in with the
// new password. It returns ErrResetLinkInvalid for a used, expired or unknown link, or one of an
// account being deleted, and the error of the password policy, which leaves the link valid.
//
// The link is checked on the reader before the transaction, so that an invalid token never takes
// the writer nor hashes a password. It is checked again in the transaction, so that of concurrent
// resets, only one replaces the password.
func (p PasswordResets) Reset(ctx context.Context, st store.Store, token, password string) error {
	if _, err := p.validLink(ctx, st, token); err != nil {
		return err
	}

	hash, err := p.accounts.checkAndHashPassword(password)
	if err != nil {
		return err
	}

	return st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		link, err := p.validLink(ctx, tx, token)
		if err != nil {
			return err
		}

		if _, err := changeableUser(ctx, tx.Users(), link.UserID); err != nil {
			if errors.Is(err, ErrUserDeleting) {
				return ErrResetLinkInvalid
			}

			return err
		}

		err = tx.Invitations().MarkUsed(ctx, link.ID, link.UserID)
		if errors.Is(err, store.ErrNotFound) {
			return ErrResetLinkInvalid
		}

		if err != nil {
			return err
		}

		if err := tx.Users().SetPasswordHash(ctx, link.UserID, hash); err != nil {
			return err
		}

		if _, err := p.sessions.RevokeAll(ctx, tx, link.UserID); err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.PasswordResetCompleted(link.UserID))
	})
}

// validLink returns the reset link of token, read through repos, or ErrResetLinkInvalid when it is
// used, expired, unknown or an invitation.
func (p PasswordResets) validLink(ctx context.Context, repos store.Repositories, token string) (store.Invitation, error) {
	link, err := repos.Invitations().GetByTokenHash(ctx, hashLinkToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.Invitation{}, ErrResetLinkInvalid
	}

	if err != nil {
		return store.Invitation{}, err
	}

	if link.Kind != store.InvitationReset || !link.UsedAt.IsZero() || p.clk.Now().After(link.ExpiresAt) {
		return store.Invitation{}, ErrResetLinkInvalid
	}

	return link, nil
}
