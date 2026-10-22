// Package cli implements the verve command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/version"
)

// Deps holds what commands need from the outside world. It grows as commands are added.
type Deps struct {
	Version version.Info
}

// Execute runs the command line with the given arguments and returns the process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return execute(ctx, Deps{Version: version.Get()}, args, stdout, stderr)
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

	root.AddCommand(newVersionCommand(deps.Version))

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
