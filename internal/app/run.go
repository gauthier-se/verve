package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/httpapi"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/version"
	"github.com/gauthier-se/verve/internal/webui"
)

// Run logs the configuration warnings, reads the embedded web app, creates the data directory
// layout, takes its lock, opens the database, loads or generates the secret key and starts serving
// HTTP. It then backs up the database and applies the pending migrations, during which only the
// health probes are served, writes the setup link on stderr while no user exists, starts the job
// worker, marks the server ready and serves until ctx is cancelled.
// Resources are released in reverse order: the database is closed once the server and the worker
// have stopped.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, stderr io.Writer) error {
	return run(ctx, cfg, logger, stderr, migrationPlan{}, jobs.NewRegistry())
}

// run is Run with the migrations of plan and the job kinds of registry.
func run(ctx context.Context, cfg config.Config, logger *slog.Logger, stderr io.Writer, plan migrationPlan,
	registry *jobs.Registry,
) error {
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

	// Nothing encrypts with the key yet: backups need the file to exist.
	if _, err := ensureSecretKey(newLayout(cfg.DataDir).SecretKey, cfg.SecretKey); err != nil {
		return err
	}

	clk := clock.Real()
	startup := newReadiness(newDatabaseChecker(db), newMigrationsChecker(db), newDataDirChecker(cfg.DataDir, clk))

	// Built before the migrations because the routes take it, but the gate keeps them closed until
	// the schema is migrated. Not closed on its own: the deferred closeDB closes the database under it.
	st := sqlite.New(db, clk)

	// Issued once the schema is migrated, when no user exists.
	setupToken := new(auth.SetupToken)

	// Built before the routes, which queue jobs on it, and started once the schema is migrated.
	schedules := registerJobs(registry, st, clk, cfg)
	worker := jobs.NewWorker(st, registry, clk, logger, jobs.Options{Schedules: schedules})

	handler := httpapi.NewHandler(httpapi.Deps{
		Logger:         logger,
		Clock:          clk,
		TrustedProxies: cfg.TrustedProxies,
		Store:          st,
		Version:        version.Get(),
		Checkers:       startup.checkers,
		Gate:           startup.gate,
		SPA:            spa,
		HSTS:           cfg.PublicURL.HTTPS(),
		PublicURL:      cfg.PublicURL,
		SetupToken:     setupToken,
		Jobs:           worker,
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

	if _, err := migrateDB(ctx, cfg, db, clk, logger, plan); err != nil {
		stopServer()

		return errors.Join(err, <-served)
	}

	if err := offerSetup(ctx, st.Users(), setupToken, cfg.PublicURL, stderr); err != nil {
		stopServer()

		return errors.Join(err, <-served)
	}

	var workerDone sync.WaitGroup

	workerDone.Go(func() { worker.Run(serveCtx) })

	startup.markReady()
	logger.Info("server ready")

	err = <-served

	// A server that failed on its own has not cancelled the worker yet.
	stopServer()
	workerDone.Wait()

	return err
}
