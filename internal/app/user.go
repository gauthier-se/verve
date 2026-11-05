package app

import (
	"context"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// CreateUser creates account in the database of cfg and records its creation from the command line.
// It opens the database in shared mode, so that it also runs next to a serving process, whose setup
// it then completes (FR-2 of accounts and administration).
func CreateUser(ctx context.Context, cfg config.Config, account auth.NewAccount) (store.User, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) (store.User, error) {
		return auth.NewAccounts(auth.DefaultHasher(), auth.NewPasswordPolicy()).Create(ctx, s, account, audit.SourceCLI)
	})
}
