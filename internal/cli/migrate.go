package cli

import (
	"context"
	"fmt"
	"log/slog"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
)

// migrateFunc applies the pending migrations of the configured database.
type migrateFunc func(ctx context.Context, cfg config.Config, logger *slog.Logger) (app.MigrateResult, error)

// migrationStatusFunc lists the migrations of the configured database.
type migrationStatusFunc func(ctx context.Context, cfg config.Config) ([]app.MigrationState, error)

func newMigrateCommand(loadConfig func() (config.Config, error), deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations (also done automatically by serve)",
		Args:  cobra.NoArgs,
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			res, err := deps.Migrate(cmd.Context(), cfg, logging.New(cmd.ErrOrStderr(), cfg.Log))
			if err != nil {
				return err
			}

			printMigrateResult(cmd, res)

			return nil
		}),
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "List the migrations with the time they were applied",
		Args:  cobra.NoArgs,
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			states, err := deps.MigrationStatus(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			return printMigrationStatus(cmd, states)
		}),
	})

	return cmd
}

func printMigrateResult(cmd *cobra.Command, res app.MigrateResult) {
	out := cmd.OutOrStdout()

	if len(res.Applied) == 0 {
		_, _ = fmt.Fprintf(out, "Database schema up to date (version %d)\n", res.Version)

		return
	}

	for _, m := range res.Applied {
		_, _ = fmt.Fprintf(out, "Applied %05d_%s\n", m.Version, m.Name)
	}

	noun := "migrations"
	if len(res.Applied) == 1 {
		noun = "migration"
	}

	_, _ = fmt.Fprintf(out, "Database schema migrated to version %d (%d %s applied)\n", res.Version, len(res.Applied), noun)
}

func printMigrationStatus(cmd *cobra.Command, states []app.MigrationState) error {
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
}
