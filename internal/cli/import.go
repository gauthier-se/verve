package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
	"github.com/gauthier-se/verve/internal/store"
)

// importXMLFunc imports an Apple Health export in this process, also next to a running server, and
// returns the import once it ended.
type importXMLFunc func(ctx context.Context, cfg config.Config, req app.XMLImport, logger *slog.Logger) (store.Import, error)

func newImportCommand(loadConfig func() (config.Config, error), deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import health data",
		Args:  cobra.NoArgs,
	}

	var login string

	xml := &cobra.Command{
		Use:   "xml --user <username> <path>",
		Short: "Import an Apple Health export for a user, also while the server runs",
		Long: "Import an Apple Health export, its export.zip or a bare export.xml, for a user, in this process. " +
			"The file is read in place and never changed. The command waits while another import of the user " +
			"runs, prints the progress to stderr and the report to stdout once the import ends. Interrupting it " +
			"cancels the import.",
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return &usageError{err: err}
			}

			if login == "" {
				return &usageError{err: errors.New("--user is required")}
			}

			return nil
		},
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			// The path is the one argument that Args lets through.
			return importXML(cmd, cfg, deps.ImportXML, login, cmd.Flags().Arg(0))
		}),
	}

	xml.Flags().StringVar(&login, "user", "", "username of the user who imports")

	cmd.AddCommand(xml)

	return cmd
}

// importXML imports the export at path for the user of login through importFn until the import
// ends or SIGINT or SIGTERM cancels it, and prints its progress and its report.
func importXML(cmd *cobra.Command, cfg config.Config, importFn importXMLFunc, login, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A second signal kills the process, as for serve.
	context.AfterFunc(ctx, stop)

	stderr := cmd.ErrOrStderr()

	imp, err := importFn(ctx, cfg, app.XMLImport{
		Login: login,
		Path:  abs,
		Waiting: func() {
			_, _ = fmt.Fprintf(stderr, "Waiting for another import of %s to end\n", login)
		},
		Progress: func(progress float64) {
			_, _ = fmt.Fprintf(stderr, "Imported %d%%\n", int(progress*100))
		},
	}, logging.New(stderr, cfg.Log))
	if err != nil {
		return importError(err, login)
	}

	if err := printImport(cmd.OutOrStdout(), imp); err != nil {
		return err
	}

	switch imp.Status {
	case store.ImportCompleted:
		return nil
	case store.ImportFailed:
		if n := len(imp.Report.Errors); n > 0 {
			// The reason of the failure is the last message, which the job adds when it fails.
			return fmt.Errorf("import %d failed: %s", imp.ID, imp.Report.Errors[n-1])
		}

		return fmt.Errorf("import %d failed", imp.ID)
	default:
		return fmt.Errorf("import %d %s", imp.ID, imp.Status)
	}
}

// importError returns err as what to change, for the errors of an import for the user of login that
// the person who runs it can fix, or else wrapped.
func importError(err error, login string) error {
	var pathErr *os.PathError

	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("no user %q", login)
	case errors.Is(err, auth.ErrUserDisabled):
		return fmt.Errorf("%s is disabled", login)
	case errors.Is(err, auth.ErrUserDeleting):
		return fmt.Errorf("%s is being deleted", login)
	case errors.As(err, &pathErr):
		return pathErr
	default:
		return fmt.Errorf("import xml: %w", err)
	}
}

// printImport writes the status of imp to w, then the total counters, the period and the routes of
// its report as a table, a dash standing for a missing period.
func printImport(w io.Writer, imp store.Import) error {
	_, _ = fmt.Fprintf(w, "Import %d %s\n", imp.ID, imp.Status)

	report := imp.Report
	total := report.Total
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	for _, counter := range []struct {
		name  string
		value int64
	}{
		{"received", total.Received},
		{"inserted", total.Inserted},
		{"updated", total.Updated},
		{"duplicates", total.Duplicates},
		{"blocked", total.Blocked},
		{"invalid", total.Invalid},
		{"unsupported", total.Unsupported},
		{"suspicious", total.Suspicious},
		{"warnings", total.Warnings},
		{"deleted", total.Deleted},
		{"notFound", total.NotFound},
	} {
		_, _ = fmt.Fprintf(tw, "%s\t%d\n", counter.name, counter.value)
	}

	period := "-"
	if !report.Period.Start.IsZero() {
		period = report.Period.Start.Format(time.RFC3339) + " to " + report.Period.End.Format(time.RFC3339)
	}

	_, _ = fmt.Fprintf(tw, "period\t%s\n", period)
	_, _ = fmt.Fprintf(tw, "routes\t%d\n", report.Routes)

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("print import: %w", err)
	}

	return nil
}
