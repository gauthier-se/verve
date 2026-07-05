// Command verve is the Verve server and CLI. In this slice it wires up the
// SQLite database with auto-applied migrations and exposes the `migrate` and
// `account create` subcommands. The HTTP server lands in a later slice.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gauthier-se/verve/internal/data"
)

// application is the central dependency-injection struct: everything the
// commands (and later the HTTP handlers) need hangs off it. No global state.
type application struct {
	config config
	logger *slog.Logger
	db     *sql.DB
	models data.Models
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Graceful-shutdown scaffolding: a context cancelled on SIGINT/SIGTERM,
	// threaded through the commands. Used in earnest once the server exists.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, os.Args[1:]); err != nil {
		logger.Error("verve failed", "err", err)
		os.Exit(1)
	}
}

// run performs the shared wiring — config, data dir, database, migrations —
// then dispatches to the requested subcommand.
func run(ctx context.Context, logger *slog.Logger, args []string) error {
	cfg, rest, err := parseConfig(args)
	if err != nil {
		return err
	}

	// Creating artifactsDir also creates its parent, the data dir.
	if err := os.MkdirAll(cfg.artifactsDir(), 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	db, err := data.Open(cfg.dbPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Migrations auto-apply on every startup: no manual step for the operator.
	if err := data.Migrate(ctx, db, logger); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	app := &application{
		config: cfg,
		logger: logger,
		db:     db,
		models: data.NewModels(db),
	}
	return app.dispatch(ctx, rest)
}
