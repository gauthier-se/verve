package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sync/atomic"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// setupTokenLength is the length of a setup token, in bytes: 256 bits.
const setupTokenLength = 32

// SetupToken holds the token that creates the first admin (FR-1 of accounts and administration), in
// memory only and never stored. Its zero value holds none. It is safe for concurrent use.
type SetupToken struct {
	// hash is the SHA-256 hash of the token, nil until Issue and after Revoke.
	hash atomic.Pointer[[sha256.Size]byte]
}

// SetupRequired reports whether users holds no user yet, so that the first admin is still to be
// created.
func SetupRequired(ctx context.Context, users store.UsersRepository) (bool, error) {
	count, err := users.Count(ctx)
	if err != nil {
		return false, err
	}

	return count == 0, nil
}

// Issue replaces the token with a new random one and returns it, once: only its hash is kept.
func (t *SetupToken) Issue() string {
	token := randomText(setupTokenLength)
	hash := sha256.Sum256([]byte(token))
	t.hash.Store(&hash)

	return token
}

// Matches reports whether token is the one issued last. It compares the hashes of both in constant
// time, so that neither the length nor the content of token tells how close it is. A nil SetupToken,
// or one never issued, matches no token.
func (t *SetupToken) Matches(token string) bool {
	if t == nil {
		return false
	}

	want := t.hash.Load()
	if want == nil {
		return false
	}

	got := sha256.Sum256([]byte(token))

	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// Revoke drops the token: it matches no token any more, until the next Issue. A nil SetupToken holds
// none to drop.
func (t *SetupToken) Revoke() {
	if t != nil {
		t.hash.Store(nil)
	}
}

// The errors of a setup. Their messages are the stable codes that the API answers with.
var (
	// ErrSetupCompleted is returned for a setup once a user exists, whatever its token.
	ErrSetupCompleted = errors.New("setup_completed")
	// ErrSetupTokenInvalid is returned for a setup whose token is not the setup token.
	ErrSetupTokenInvalid = errors.New("setup_token_invalid")
)

// FirstAdmin is what a client sends to create the first admin.
type FirstAdmin struct {
	Token    string
	Username string
	Password string
}

// Setup creates the first admin with the setup token (FR-1 of accounts and administration).
type Setup struct {
	token    *SetupToken
	accounts Accounts
	sessions Sessions
}

// NewSetup returns the setup service, which checks the setup token against token, creates the admin
// with accounts and opens their session with sessions.
func NewSetup(token *SetupToken, accounts Accounts, sessions Sessions) *Setup {
	return &Setup{token: token, accounts: accounts, sessions: sessions}
}

// CreateAdmin creates the admin of admin through st, records the audit event and opens their
// session, from the client of ctx as audit.MetaFrom describes it, in one transaction. It returns
// ErrSetupCompleted once a user exists, whatever the token, and otherwise ErrSetupTokenInvalid when
// the token of admin is not the setup token, then ErrUsernameInvalid or the error of the password
// policy.
//
// The count and the token are checked on the reader before the transaction, so that the requests
// that fail them never take the writer nor hash a password. The count is read again in the
// transaction that creates the admin, so that of concurrent setups, or of a setup and verve user
// create, only one creates a user. It is also read again after a token that does not match, so that
// a setup overtaken by another one, which revoked the token, returns ErrSetupCompleted as well.
//
// A setup that finds a user, or creates one, revokes the token: it stops being valid as soon as a
// user exists, and does not become valid again if no user is left, until a restart issues another.
func (s *Setup) CreateAdmin(ctx context.Context, st store.Store, admin FirstAdmin) (LoggedIn, error) {
	loggedIn, err := s.createAdmin(ctx, st, admin)
	if err == nil || errors.Is(err, ErrSetupCompleted) {
		s.token.Revoke()
	}

	return loggedIn, err
}

func (s *Setup) createAdmin(ctx context.Context, st store.Store, admin FirstAdmin) (LoggedIn, error) {
	required, err := SetupRequired(ctx, st.Users())
	if err != nil {
		return LoggedIn{}, err
	}

	if !required {
		return LoggedIn{}, ErrSetupCompleted
	}

	if !s.token.Matches(admin.Token) {
		return LoggedIn{}, tokenMismatchError(ctx, st.Users())
	}

	user, err := s.accounts.checkAndHash(NewAccount{Username: admin.Username, Password: admin.Password, Role: store.RoleAdmin})
	if err != nil {
		return LoggedIn{}, err
	}

	client := audit.MetaFrom(ctx)

	var loggedIn LoggedIn

	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		required, err := SetupRequired(ctx, tx.Users())
		if err != nil {
			return err
		}

		if !required {
			return ErrSetupCompleted
		}

		created, err := insertUser(ctx, tx, user, audit.SourceSetup)
		if err != nil {
			return err
		}

		id, session, err := s.sessions.Create(ctx, tx, NewSession{
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

// tokenMismatchError returns the error of a setup whose token does not match: ErrSetupTokenInvalid,
// or ErrSetupCompleted once users holds a user. A concurrent setup may have created the admin and
// revoked the token since the count was read: it committed the admin before it revoked the token,
// so the count read again sees them.
func tokenMismatchError(ctx context.Context, users store.UsersRepository) error {
	required, err := SetupRequired(ctx, users)
	if err != nil {
		return err
	}

	if !required {
		return ErrSetupCompleted
	}

	return ErrSetupTokenInvalid
}
