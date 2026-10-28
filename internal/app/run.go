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
// the database, applies the pending migrations, builds the store and serves HTTP until ctx is
// cancelled. Resources are released in reverse order: the database is closed once the server has
// stopped.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	for _, warning := range cfg.Warnings() {
		logger.Warn(warning)
	}

	db, closeDB, err := openDatabase(ctx, cfg.DataDir)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := closeDB(context.WithoutCancel(ctx)); closeErr != nil {
			logger.Error("close database", slog.Any("error", closeErr))

			return
		}

		logger.Info("database closed")
	}()

	if _, err := migrateDB(ctx, db, logger); err != nil {
		return err
	}

	clk := clock.Real()

	handler := httpapi.NewHandler(httpapi.Deps{
		Logger:         logger,
		Clock:          clk,
		TrustedProxies: cfg.TrustedProxies,
		// Not closed on its own: the deferred closeDB closes the database under it.
		Store: sqlite.New(db, clk),
	})
	srv := NewServer(ServerConfig{Addr: cfg.ListenAddr}, handler, logger)

	return srv.Run(ctx)
}
