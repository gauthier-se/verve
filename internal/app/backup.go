package app

import (
	"context"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/version"
)

// Backup creates a manual backup of the data directory of cfg and returns the path of its archive.
// It opens the database in shared mode, so that it also runs next to a serving process (FR-5 of
// backups).
func Backup(ctx context.Context, cfg config.Config) (string, error) {
	return withSharedDB(ctx, cfg, func(s store.Store) (string, error) {
		return newBackups(cfg, clock.Real()).Create(ctx, s, backup.ReasonManual)
	})
}

// newBackups returns the backups of the data directory of cfg, timed by clk.
func newBackups(cfg config.Config, clk clock.Clock) *backup.Backups {
	layout := newLayout(cfg.DataDir)

	// A configured key has no file to back up: the operator keeps it.
	secretKey := layout.SecretKey
	if cfg.SecretKey != "" {
		secretKey = ""
	}

	return backup.New(backup.Config{
		Dir:       layout.Backups,
		DB:        layout.DB,
		SecretKey: secretKey,
		Version:   version.Get().Version,
		FreeSpace: backup.FreeSpace,
	}, clk)
}
