package cli

import (
	"context"
	"fmt"
	"log/slog"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// migrateFunc applies the pending migrations of the configured database.
type migrateFunc func(ctx context.Context, cfg config.Config, logger *slog.Logger) (sqlite.MigrateResult, error)

// migrationStatusFunc lists the migrations of the configured database.
type migrationStatusFunc func(ctx context.Context, cfg config.Config) ([]sqlite.MigrationState, error)

func newMigrateCommand(loadConfig func() (config.Config, error), migrate migrateFunc, status migrationStatusFunc, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations (also done automatically by serve)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("invalid configuration:\n%s", bulletList(err))
			}

			res, err := migrate(cmd.Context(), cfg, logger)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if len(res.Applied) == 0 {
				_, _ = fmt.Fprintf(out, "Database schema up to date (version %d)\n", res.Version)

				return nil
			}

			for _, m := range res.Applied {
				_, _ = fmt.Fprintf(out, "Applied %05d_%s\n", m.Version, m.Name)
			}

			_, _ = fmt.Fprintf(out, "Database schema migrated to version %d (%d %s applied)\n",
				res.Version, len(res.Applied), plural(len(res.Applied), "migration", "migrations"))

			return nil
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "List the migrations with the time they were applied",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("invalid configuration:\n%s", bulletList(err))
			}

			states, err := status(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "VERSION\tNAME\tAPPLIED")

			for _, s := range states {
				applied := "pending"
				if !s.AppliedAt.IsZero() {
					applied = s.AppliedAt.UTC().Format(time.RFC3339)
				}

				_, _ = fmt.Fprintf(w, "%05d\t%s\t%s\n", s.Version, s.Name, applied)
			}

			return w.Flush()
		},
	})

	return cmd
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}
