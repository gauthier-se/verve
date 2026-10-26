// Package app wires the Verve server: data directory, lock, HTTP handler and server lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Default server timeouts.
const (
	defaultShutdownTimeout   = 30 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// ServerConfig configures a Server. Zero durations take their defaults. There is no global
// write timeout: uploads and ingestion need long requests.
type ServerConfig struct {
	Addr              string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
}

// Server is an HTTP server that shuts down gracefully when its context is cancelled.
type Server struct {
	cfg     ServerConfig
	handler http.Handler
	logger  *slog.Logger
	ready   chan net.Addr
}

// NewServer returns a server for h. Run starts it.
func NewServer(cfg ServerConfig, h http.Handler, logger *slog.Logger) *Server {
	cfg.ShutdownTimeout = orDefault(cfg.ShutdownTimeout, defaultShutdownTimeout)
	cfg.ReadHeaderTimeout = orDefault(cfg.ReadHeaderTimeout, defaultReadHeaderTimeout)
	cfg.IdleTimeout = orDefault(cfg.IdleTimeout, defaultIdleTimeout)

	return &Server{cfg: cfg, handler: h, logger: logger, ready: make(chan net.Addr, 1)}
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}

	return d
}

// Ready receives the bound address once the server listens.
func (s *Server) Ready() <-chan net.Addr {
	return s.ready
}

// Run listens on the configured address and serves until ctx is cancelled. It then stops
// accepting connections and waits for in-flight requests up to the shutdown timeout, after
// which the remaining connections are closed. It returns nil after a shutdown.
func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Addr, err)
	}

	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(s.logger.Handler(), slog.LevelWarn),
	}

	s.logger.Info("server listening", slog.String("addr", ln.Addr().String()))
	s.ready <- ln.Addr()

	served := make(chan error, 1)

	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	s.logger.Info("server shutting down", slog.Duration("timeout", s.cfg.ShutdownTimeout))

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("shut down server: %w", err)
		}

		s.logger.Warn("shutdown timeout expired, closing remaining connections",
			slog.Duration("timeout", s.cfg.ShutdownTimeout))

		if err := srv.Close(); err != nil {
			return fmt.Errorf("close server: %w", err)
		}
	}

	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	s.logger.Info("server stopped")

	return nil
}
