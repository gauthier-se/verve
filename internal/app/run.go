package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/httpapi"
)

// dataDirPerm is the permission of the data directory when Verve creates it.
const dataDirPerm = 0o750

// Run logs the configuration warnings, creates the data directory, takes its lock and serves
// HTTP until ctx is cancelled. Resources are released in reverse order.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	for _, warning := range cfg.Warnings() {
		logger.Warn(warning)
	}

	if err := os.MkdirAll(cfg.DataDir, dataDirPerm); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	lock, err := AcquireLock(cfg.DataDir)
	if err != nil {
		return err
	}

	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			logger.Error("release data directory lock", slog.Any("error", releaseErr))
		}
	}()

	handler := httpapi.NewHandler(httpapi.Deps{
		Logger:         logger,
		Clock:          clock.Real(),
		TrustedProxies: cfg.TrustedProxies,
	})
	srv := NewServer(ServerConfig{Addr: cfg.ListenAddr}, handler, logger)

	return srv.Run(ctx)
}
