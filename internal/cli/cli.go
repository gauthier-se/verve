// Package cli implements the verve command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/version"
)

// Deps holds what commands need from the outside world. It grows as commands are added.
type Deps struct {
	Version version.Info
	// LoadConfig loads the configuration with the file given by --config (empty when unset).
	LoadConfig func(file string) (config.Config, error)
	// Serve runs the server until ctx is cancelled.
	Serve serveFunc
	// Migrate applies the pending database migrations.
	Migrate migrateFunc
	// MigrationStatus lists the database migrations.
	MigrationStatus migrationStatusFunc
	// CreateUser creates a user, also next to a running server.
	CreateUser createUserFunc
	// ListUsers, SetUserStatus and ResetPassword manage the users, also next to a running server.
	ListUsers     listUsersFunc
	SetUserStatus setUserStatusFunc
	ResetPassword resetPasswordFunc
	// Backup creates a manual backup and returns its path, also next to a running server.
	Backup backupFunc
	// Restore restores a backup archive while the server is stopped.
	Restore restoreFunc
	// ReadPassword reads a password without echo from the terminal of stdin. Nil means that stdin is
	// not a terminal.
	ReadPassword passwordFunc
	// HealthcheckTimeout is how long healthcheck waits for the server, 3 seconds when zero.
	HealthcheckTimeout time.Duration
}

// Execute runs the command line with the given arguments, reading os.Stdin, and returns the process
// exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	environ := environMap(os.Environ())
	deps := Deps{
		Version: version.Get(),
		LoadConfig: func(file string) (config.Config, error) {
			return config.Load(config.LoadOptions{File: file, Env: environ})
		},
		Serve:           app.Run,
		Migrate:         app.Migrate,
		MigrationStatus: app.MigrationStatus,
		CreateUser:      app.CreateUser,
		ListUsers:       app.ListUsers,
		SetUserStatus:   app.SetUserStatus,
		ResetPassword:   app.ResetPassword,
		Backup:          app.Backup,
		Restore:         app.Restore,
		ReadPassword:    terminalPassword(os.Stdin),
	}

	return execute(ctx, deps, args, os.Stdin, stdout, stderr)
}

// environMap converts KEY=value pairs, as returned by os.Environ, into a map.
func environMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		key, value, _ := strings.Cut(kv, "=")
		m[key] = value
	}

	return m
}

func execute(ctx context.Context, deps Deps, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := NewRootCommand(deps)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)

		var usageErr *usageError
		if errors.As(err, &usageErr) {
			_, _ = fmt.Fprintf(stderr, "Run '%s --help' for usage.\n", root.Name())
		}

		return 1
	}

	return 0
}

// NewRootCommand builds the verve command tree.
func NewRootCommand(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "verve",
		Short:         "Self-hosted health dashboard for Apple Health data",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &usageError{err: fmt.Errorf("unknown command %q", args[0])}
			}

			return nil
		},
	}

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	var configFile string
	root.PersistentFlags().StringVar(&configFile, "config", "", "path to a YAML configuration file (default $VERVE_CONFIG)")

	loadConfig := func() (config.Config, error) {
		return deps.LoadConfig(configFile)
	}

	// Without arguments, verve behaves exactly like `verve serve`.
	runServe := serveRunner(loadConfig, deps.Serve)
	root.RunE = runServe

	root.AddCommand(newServeCommand(runServe))
	root.AddCommand(newMigrateCommand(loadConfig, deps))
	root.AddCommand(newUserCommand(loadConfig, deps))
	root.AddCommand(newBackupCommand(loadConfig, deps))
	root.AddCommand(newRestoreCommand(loadConfig, deps))
	root.AddCommand(newVersionCommand(deps.Version))
	root.AddCommand(newConfigCommand(loadConfig))
	root.AddCommand(newHealthcheckCommand(loadConfig, deps.HealthcheckTimeout))
	root.AddCommand(newOpenAPICommand())

	return root
}

// usageError marks an invalid command line, which deserves a hint about --help.
type usageError struct {
	err error
}

func (e *usageError) Error() string {
	return e.err.Error()
}

func (e *usageError) Unwrap() error {
	return e.err
}
