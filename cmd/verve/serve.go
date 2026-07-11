package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/api"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/web"
)

// serveCommand starts the HTTP server and blocks until the context is cancelled
// (SIGINT/SIGTERM, wired in main), then shuts down gracefully so in-flight
// requests finish. Requests authenticate via a session cookie (ADR 0008);
// --secure-cookie=false relaxes the cookie's Secure attribute for plain-HTTP
// local development.
func (app *application) serveCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "address to listen on")
	secureCookie := fs.Bool("secure-cookie", true, "set the Secure attribute on the session cookie (disable only for plain-HTTP local dev)")
	maxUploadMB := fs.Int64("max-upload-mb", 0, "reject a web import upload larger than this many MiB (0 uses the built-in default)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	server, err := api.New(app.logger, app.models, query.Engine{DB: app.db}, api.Config{
		SecureCookies:  *secureCookie,
		SPA:            web.Handler(),
		DataDir:        app.config.dataDir,
		ArtifactsDir:   app.config.artifactsDir(),
		MaxUploadBytes: *maxUploadMB << 20,
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:    *addr,
		Handler: server.Handler(),
		// A web import streams a multi-hundred-MB upload through the request body,
		// so a whole-request read/write deadline would abort it (ADR 0016); the
		// header deadline still guards slowloris. Import runs in the background, off
		// the response.
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
	}

	// Surface a listen failure (e.g. port in use) through this channel so the
	// command returns it instead of blocking forever on shutdown.
	listenErr := make(chan error, 1)
	go func() {
		app.logger.Info("http server listening", "addr", *addr, "secure_cookie", *secureCookie, "version", version)
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
