package app

import (
	"context"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// usersPageSize is the number of users that ListUsers reads at a time.
const usersPageSize = 200

// The user commands open the database in shared mode, so that they also run next to a serving
// process (FR-2 of accounts and administration). They act as the system: their audit events have
// no actor and no client.

// CreateUser creates account in the database of cfg and records its creation from the command line.
// Creating the first admin this way completes the setup of a serving process.
func CreateUser(ctx context.Context, cfg config.Config, account auth.NewAccount) (store.User, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) (store.User, error) {
		return newAccounts().Create(ctx, s, account, audit.SourceCLI)
	})
}

// ListUsers returns every user of the database of cfg, newest first.
func ListUsers(ctx context.Context, cfg config.Config) ([]store.User, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) ([]store.User, error) {
		var all []store.User

		page := store.PageRequest{Limit: usersPageSize}

		for {
			users, err := s.Users().List(ctx, page)
			if err != nil {
				return nil, err
			}

			all = append(all, users...)

			if len(users) < page.Limit {
				return all, nil
			}

			page.After = users[len(users)-1].Cursor()
		}
	})
}

// SetUserStatus enables or disables the user of login in the database of cfg, with the rules of the
// API, and returns the user. It returns store.ErrNotFound when no user has login, and
// auth.ErrLastAdmin for the last active admin.
func SetUserStatus(ctx context.Context, cfg config.Config, login string, status store.UserStatus) (store.User, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) (store.User, error) {
		user, err := s.Users().GetByLogin(ctx, login)
		if err != nil {
			return store.User{}, err
		}

		return newAdmin().SetStatus(ctx, s, auth.StatusChange{UserID: user.ID, Status: status})
	})
}

// ResetPassword replaces the password of the user of login in the database of cfg and revokes their
// sessions, and returns the user. It returns store.ErrNotFound when no user has login, and the
// error of the password policy.
func ResetPassword(ctx context.Context, cfg config.Config, login, password string) (store.User, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) (store.User, error) {
		user, err := s.Users().GetByLogin(ctx, login)
		if err != nil {
			return store.User{}, err
		}

		return user, newAdmin().ResetPassword(ctx, s, auth.PasswordReset{UserID: user.ID, Password: password})
	})
}

// newAccounts returns the accounts service of the command line.
func newAccounts() auth.Accounts {
	return auth.NewAccounts(auth.DefaultHasher(), auth.NewPasswordPolicy())
}

// newAdmin returns the admin service of the command line.
func newAdmin() auth.Admin {
	return auth.NewAdmin(newAccounts(), auth.NewSessions(clock.Real()))
}
