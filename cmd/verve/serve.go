package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/api"
	"github.com/gauthier-se/verve/internal/query"
)

// serveCommand starts the HTTP server and blocks until the context is cancelled
// (SIGINT/SIGTERM, wired in main), then shuts down gracefully so in-flight
// requests finish. Until auth lands (slice 05), --account sets the dev Account
// every request is scoped to unless it sends an X-Verve-Account header.
func (app *application) serveCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "address to listen on")
	account := fs.String("account", "", "dev Account email requests default to (overridable per request by the X-Verve-Account header)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	server := api.New(app.logger, app.models, query.Engine{DB: app.db}, *account)
	srv := &http.Server{
		Addr:         *addr,
		Handler:      server.Handler(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  time.Minute,
	}

	// Surface a listen failure (e.g. port in use) through this channel so the
	// command returns it instead of blocking forever on shutdown.
	listenErr := make(chan error, 1)
	go func() {
		app.logger.Info("http server listening", "addr", *addr, "dev_account", *account)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
	}()

	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
		app.logger.Info("shutting down http server")
		// Give in-flight requests a bounded window to finish; Shutdown stops
		// accepting new connections immediately.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
