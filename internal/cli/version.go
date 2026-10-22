package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/version"
)

func newVersionCommand(info version.Info) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			if asJSON {
				return json.NewEncoder(out).Encode(info)
			}

			_, err := fmt.Fprintln(out, info.String())

			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the build metadata as JSON")

	return cmd
}
