package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
)

// serveFunc runs the server until ctx is cancelled.
type serveFunc func(ctx context.Context, cfg config.Config, logger *slog.Logger) error

func newServeCommand(runServe func(cmd *cobra.Command, args []string) error) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server (the default command)",
		Args:  cobra.NoArgs,
		RunE:  runServe,
	}
}

// serveRunner loads the configuration and runs serve, logging to stderr as configured, until
// SIGINT or SIGTERM. After the first signal the default behavior is restored, so a second signal
// kills the process.
func serveRunner(loadConfig func() (config.Config, error), serve serveFunc) func(*cobra.Command, []string) error {
	return withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		context.AfterFunc(ctx, stop)

		return serve(ctx, cfg, logging.New(cmd.ErrOrStderr(), cfg.Log))
	})
}
