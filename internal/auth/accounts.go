package auth

import (
	"context"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// NewAccount describes an account to create, which logs in with a password.
type NewAccount struct {
	Username string
	// Email is empty for none. The store requires an @ in it.
	Email    string
	Password string
	Role     store.UserRole
}

// Accounts creates the accounts that log in with a password.
type Accounts struct {
	hasher PasswordHasher
	policy PasswordPolicy
}

// NewAccounts returns the accounts service, which checks passwords against policy and hashes them
// with hasher.
func NewAccounts(hasher PasswordHasher, policy PasswordPolicy) Accounts {
	return Accounts{hasher: hasher, policy: policy}
}

// Create checks account against the rules of a username and the password policy, hashes its
// password, and stores the active user through st with the audit event of its creation from source,
// in one transaction. It returns ErrUsernameInvalid, the error of the password policy, or
// store.ErrConflict when the username is taken.
func (a Accounts) Create(ctx context.Context, st store.Store, account NewAccount, source audit.UserSource) (store.User, error) {
	user, err := a.checkAndHash(account)
	if err != nil {
		return store.User{}, err
	}

	var created store.User

	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		created, err = insertUser(ctx, tx, user, source)

		return err
	})

	return created, err
}

// checkAndHash checks account against the rules of a username and the password policy, and returns
// the user to store, with the hash of its password, or ErrUsernameInvalid or the error of the
// password policy. It runs outside any transaction, so that the writer is not held while it hashes.
func (a Accounts) checkAndHash(account NewAccount) (store.NewUser, error) {
	if err := ValidateUsername(account.Username); err != nil {
		return store.NewUser{}, err
	}

	hash, err := a.checkAndHashPassword(account.Password)
	if err != nil {
		return store.NewUser{}, err
	}

	return store.NewUser{
		Username:     account.Username,
		Email:        account.Email,
		PasswordHash: hash,
		Role:         account.Role,
	}, nil
}

// insertUser stores user through repos and records its creation from source.
func insertUser(ctx context.Context, repos store.Repositories, user store.NewUser, source audit.UserSource) (store.User, error) {
	created, err := repos.Users().Create(ctx, user)
	if err != nil {
		return store.User{}, err
	}

	if err := audit.Record(ctx, repos.AuditEvents(), audit.UserCreated(created.ID, created.Role, source)); err != nil {
		return store.User{}, err
	}

	return created, nil
}

// checkAndHashPassword checks password against the password policy and returns its hash, or the
// error of the policy.
func (a Accounts) checkAndHashPassword(password string) (string, error) {
	if err := a.policy.Check(password); err != nil {
		return "", err
	}

	return a.hasher.Hash(password), nil
}
