package app

import (
	"context"
	"log/slog"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/httpapi"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// Run logs the configuration warnings, creates the data directory layout, takes its lock, opens
// and migrates the database and serves HTTP until ctx is cancelled. Resources are released in reverse order:
// the database is closed once the server has stopped.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	for _, warning := range cfg.Warnings() {
		logger.Warn(warning)
	}

	layout, err := EnsureDataDir(cfg.DataDir)
	if err != nil {
		return err
	}

	lock, err := AcquireLock(layout.Root)
	if err != nil {
		return err
	}

	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			logger.Error("release data directory lock", slog.Any("error", releaseErr))
		}
	}()

	db, err := sqlite.Open(ctx, layout.DB)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := db.Close(context.WithoutCancel(ctx)); closeErr != nil {
			logger.Error("close database", slog.Any("error", closeErr))

			return
		}

		logger.Info("database closed")
	}()

	if _, err := migrateDB(ctx, db, logger); err != nil {
		return err
	}

	handler := httpapi.NewHandler(httpapi.Deps{
		Logger:         logger,
		Clock:          clock.Real(),
		TrustedProxies: cfg.TrustedProxies,
	})
	srv := NewServer(ServerConfig{Addr: cfg.ListenAddr}, handler, logger)

	return srv.Run(ctx)
}
