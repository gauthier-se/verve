package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

const (
	// linkTokenLength is the length of the token of a single-use link, in bytes: 256 bits.
	linkTokenLength = 32
	// invitationLifetime is how long an invitation stays valid (FR-4 of accounts and
	// administration).
	invitationLifetime = 72 * time.Hour
)

// The errors of an invitation. Their messages are the stable codes that the API answers with.
var (
	// ErrInvitationInvalid is returned for a token that no unused, unexpired invitation has.
	ErrInvitationInvalid = errors.New("invitation_invalid")
	// ErrUsernameTaken is returned for an invitation whose username a user has, ignoring case.
	ErrUsernameTaken = errors.New("username_taken")
	// ErrEmailTaken is returned for an invitation whose email a user has, ignoring case.
	ErrEmailTaken = errors.New("email_taken")
)

// NewInvitation is what an admin sends to invite someone.
type NewInvitation struct {
	Username string
	// Email is empty for none.
	Email string
	Role  store.UserRole
	// AdminID is the admin who invites.
	AdminID int64
}

// CreatedInvitation is a stored invitation and the token of its link, returned once: the store only
// holds its hash.
type CreatedInvitation struct {
	Invitation store.Invitation
	Token      string
}

// Invitations invites users by a single-use link, through which they choose their password (FR-4
// of accounts and administration).
type Invitations struct {
	clk      clock.Clock
	accounts Accounts
	sessions Sessions
}

// NewInvitations returns the invitations service, which takes the current time from clk, creates
// the accounts with accounts and opens their session with sessions.
func NewInvitations(clk clock.Clock, accounts Accounts, sessions Sessions) Invitations {
	return Invitations{clk: clk, accounts: accounts, sessions: sessions}
}

// Create stores an invitation valid 72 hours through st, with the audit event of its creation from
// the client of ctx, in one transaction, and returns it with the token of its link. It returns
// ErrUsernameInvalid or ErrEmailInvalid for a login that breaks the rules, and ErrUsernameTaken or
// ErrEmailTaken for one that a user has.
func (i Invitations) Create(ctx context.Context, st store.Store, invitation NewInvitation) (CreatedInvitation, error) {
	if err := ValidateUsername(invitation.Username); err != nil {
		return CreatedInvitation{}, err
	}

	if invitation.Email != "" {
		if err := ValidateEmail(invitation.Email); err != nil {
			return CreatedInvitation{}, err
		}
	}

	token := randomText(linkTokenLength)

	var created store.Invitation

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := loginTaken(ctx, tx.Users(), invitation.Username, invitation.Email); err != nil {
			return err
		}

		var err error

		created, err = tx.Invitations().Create(ctx, store.NewInvitation{
			Kind:      store.InvitationInvite,
			TokenHash: hashLinkToken(token),
			Username:  invitation.Username,
			Email:     invitation.Email,
			Role:      invitation.Role,
			ExpiresAt: i.clk.Now().Add(invitationLifetime),
			CreatedBy: invitation.AdminID,
		})
		if err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.UserInvited(invitation.AdminID, created.Username, created.Role))
	})
	if err != nil {
		return CreatedInvitation{}, err
	}

	return CreatedInvitation{Invitation: created, Token: token}, nil
}

// Inspect returns the invitation of token, read through repos, or ErrInvitationInvalid when it is
// used, expired or unknown.
func (i Invitations) Inspect(ctx context.Context, repos store.Repositories, token string) (store.Invitation, error) {
	return validLink(ctx, repos, i.clk, token, store.InvitationInvite, ErrInvitationInvalid)
}

// validLink returns the single-use link of token and kind, read through repos, or invalid when it
// is used, expired at the time of clk, unknown or of another kind.
func validLink(
	ctx context.Context, repos store.Repositories, clk clock.Clock, token string, kind store.InvitationKind, invalid error,
) (store.Invitation, error) {
	link, err := repos.Invitations().GetByTokenHash(ctx, hashLinkToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.Invitation{}, invalid
	}

	if err != nil {
		return store.Invitation{}, err
	}

	if link.Kind != kind || !link.UsedAt.IsZero() || clk.Now().After(link.ExpiresAt) {
		return store.Invitation{}, invalid
	}

	return link, nil
}

// Accept creates the user of the invitation of token with password through st, marks the
// invitation used, records the audit event and opens the session of the user, from the client of
// ctx, in one transaction. It returns ErrInvitationInvalid for a used, expired or unknown
// invitation, the error of the password policy, and ErrUsernameTaken or ErrEmailTaken when a user
// took the login of the invitation since its creation.
//
// The invitation is checked on the reader before the transaction, so that an invalid token never
// takes the writer nor hashes a password. It is checked again in the transaction, so that of
// concurrent accepts, only one creates the user.
func (i Invitations) Accept(ctx context.Context, st store.Store, token, password string) (LoggedIn, error) {
	invitation, err := i.Inspect(ctx, st, token)
	if err != nil {
		return LoggedIn{}, err
	}

	user, err := i.accounts.checkAndHash(NewAccount{
		Username: invitation.Username,
		Email:    invitation.Email,
		Password: password,
		Role:     invitation.Role,
	})
	if err != nil {
		return LoggedIn{}, err
	}

	client := audit.MetaFrom(ctx)

	var loggedIn LoggedIn

	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if _, err := i.Inspect(ctx, tx, token); err != nil {
			return err
		}

		if err := loginTaken(ctx, tx.Users(), user.Username, user.Email); err != nil {
			return err
		}

		created, err := insertUser(ctx, tx, user, audit.SourceInvitation)
		if err != nil {
			return err
		}

		err = tx.Invitations().MarkUsed(ctx, invitation.ID, created.ID)
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvitationInvalid
		}

		if err != nil {
			return err
		}

		id, session, err := i.sessions.Create(ctx, tx, NewSession{
			UserID:    created.ID,
			Method:    store.LoginPassword,
			ClientIP:  ipText(client.IP),
			UserAgent: client.UserAgent,
		})
		if err != nil {
			return err
		}

		loggedIn = LoggedIn{User: created, SessionID: id, Session: session}

		return nil
	})

	return loggedIn, err
}

// loginTaken returns ErrUsernameTaken when a user of users has username, and ErrEmailTaken when one
// has email, ignoring case. An empty email is taken by no one.
func loginTaken(ctx context.Context, users store.UsersRepository, username, email string) error {
	logins := []struct {
		login string
		taken error
	}{{username, ErrUsernameTaken}, {email, ErrEmailTaken}}

	for _, l := range logins {
		if l.login == "" {
			continue
		}

		_, err := users.GetByLogin(ctx, l.login)

		switch {
		case err == nil:
			return l.taken
		case !errors.Is(err, store.ErrNotFound):
			return err
		}
	}

	return nil
}

// hashLinkToken returns the SHA-256 hash of the token of a single-use link, as the client sends it.
func hashLinkToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}
