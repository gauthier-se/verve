package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/httpapi"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/version"
	"github.com/gauthier-se/verve/internal/webui"
)

// Run logs the configuration warnings, reads the embedded web app, creates the data directory
// layout, takes its lock, opens the database and starts serving HTTP. It then applies the pending
// migrations, during which only the health probes are served, marks the server ready and serves
// until ctx is cancelled.
// Resources are released in reverse order: the database is closed once the server has stopped.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	return run(ctx, cfg, logger, beforeMigrate)
}

// run is Run with hook called before the pending migrations are applied.
func run(ctx context.Context, cfg config.Config, logger *slog.Logger, hook beforeMigrateFunc) error {
	for _, warning := range cfg.Warnings() {
		logger.Warn(warning)
	}

	spa, err := httpapi.NewSPAHandler(webui.FS())
	if err != nil {
		return err
	}

	if !webui.Built() {
		logger.Warn("web app not built, serving a placeholder page: run task build to embed it")
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

	clk := clock.Real()
	startup := newReadiness(newDatabaseChecker(db), newMigrationsChecker(db), newDataDirChecker(cfg.DataDir, clk))

	handler := httpapi.NewHandler(httpapi.Deps{
		Logger:         logger,
		Clock:          clk,
		TrustedProxies: cfg.TrustedProxies,
		// Built before the migrations because the routes take it, but the gate keeps them closed
		// until the schema is migrated. Not closed on its own: the deferred closeDB closes the
		// database under it.
		Store:    sqlite.New(db, clk),
		Version:  version.Get(),
		Checkers: startup.checkers,
		Gate:     startup.gate,
		SPA:      spa,
	})
	srv := NewServer(ServerConfig{Addr: cfg.ListenAddr}, handler, logger)

	serveCtx, stopServer := context.WithCancel(ctx)
	defer stopServer()

	served := make(chan error, 1)

	go func() { served <- srv.Run(serveCtx) }()

	select {
	case <-srv.Ready():
	case err := <-served:
		return err
	}

	if _, err := migrateDB(ctx, db, logger, hook); err != nil {
		stopServer()

		return errors.Join(err, <-served)
	}

	startup.markReady()
	logger.Info("server ready")

	return <-served
}
