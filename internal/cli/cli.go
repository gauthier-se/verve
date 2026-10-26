// Package cli implements the verve command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/version"
)

// Deps holds what commands need from the outside world. It grows as commands are added.
type Deps struct {
	Version version.Info
	// LoadConfig loads the configuration with the file given by --config (empty when unset).
	LoadConfig func(file string) (config.Config, error)
}

// Execute runs the command line with the given arguments and returns the process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	environ := environMap(os.Environ())
	deps := Deps{
		Version: version.Get(),
		LoadConfig: func(file string) (config.Config, error) {
			return config.Load(config.LoadOptions{File: file, Env: environ})
		},
	}

	return execute(ctx, deps, args, stdout, stderr)
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

func execute(ctx context.Context, deps Deps, args []string, stdout, stderr io.Writer) int {
	root := NewRootCommand(deps)
	root.SetArgs(args)
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
		// Without arguments the help is printed for now: a later story starts the server instead.
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &usageError{err: fmt.Errorf("unknown command %q", args[0])}
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
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

	root.AddCommand(newVersionCommand(deps.Version))
	root.AddCommand(newConfigCommand(loadConfig))

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
