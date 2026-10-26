package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/config"
)

func newConfigCommand(loadConfig func() (config.Config, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the configuration",
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Validate the configuration and print the effective values with their sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("invalid configuration:\n%s", bulletList(err))
			}

			for _, warning := range cfg.Warnings() {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
			}

			return cfg.WriteYAML(cmd.OutOrStdout())
		},
	})

	return cmd
}

// bulletList prints each error joined with errors.Join on its own line.
func bulletList(err error) string {
	errs := []error{err}

	if multi, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok {
		errs = multi.Unwrap()
	}

	var sb strings.Builder
	for _, e := range errs {
		_, _ = fmt.Fprintf(&sb, "  - %s\n", e)
	}

	return strings.TrimSuffix(sb.String(), "\n")
}
