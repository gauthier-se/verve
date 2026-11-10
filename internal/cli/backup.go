package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/config"
)

// backupFunc creates a manual backup of the configured data directory, also next to a running
// server, and returns the path of its archive.
type backupFunc func(ctx context.Context, cfg config.Config) (string, error)

func newBackupCommand(loadConfig func() (config.Config, error), deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Back up the database and the secret key, also while the server runs",
		Long: "Back up the database and the secret key into a compressed archive of the backups directory, and " +
			"print its path. The archive holds the secret key, so keep it as safe as the data directory.",
		Args: cobra.NoArgs,
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			path, err := deps.Backup(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)

			return nil
		}),
	}
}
