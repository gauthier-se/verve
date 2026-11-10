package cli

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
)

// restoreFunc restores a backup archive into the configured data directory, while the server is
// stopped.
type restoreFunc func(ctx context.Context, cfg config.Config, archive string, logger *slog.Logger) (app.RestoreResult, error)

func newRestoreCommand(loadConfig func() (config.Config, error), deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <file>",
		Short: "Restore a backup archive (stop the server first)",
		Long: "Restore the database and the secret key of a backup archive, after keeping the current ones as a " +
			"pre-restore backup, then apply the migrations of a backup from an older version. The server must be " +
			"stopped. An invalid archive, or one from a newer version, is refused and nothing changes. An archive " +
			"without secret key keeps the current key.",
		Args: cobra.ExactArgs(1),
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			// The file is the one argument that cobra.ExactArgs lets through.
			archive := cmd.Flags().Arg(0)

			res, err := deps.Restore(cmd.Context(), cfg, archive, logging.New(cmd.ErrOrStderr(), cfg.Log))
			if err != nil {
				return err
			}

			printRestoreResult(cmd, filepath.Base(archive), res)

			return nil
		}),
	}
}

func printRestoreResult(cmd *cobra.Command, name string, res app.RestoreResult) {
	out := cmd.OutOrStdout()

	if res.PreRestore != "" {
		_, _ = fmt.Fprintf(out, "Backed up the current database to %s\n", res.PreRestore)
	}

	if res.SecretKey {
		_, _ = fmt.Fprintf(out, "Restored the database and the secret key of %s\n", name)
	} else {
		_, _ = fmt.Fprintf(out, "Restored the database of %s, which holds no secret key: the current key stays\n", name)
	}

	printMigrateResult(cmd, res.Migrations)
}
