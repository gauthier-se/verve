package cli

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/httpapi"
)

// newOpenAPICommand prints the spec of an API built without dependencies: it loads no
// configuration and opens neither the data directory nor the database, so that the frontend
// build needs no running instance.
func newOpenAPICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec of the API as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spec := httpapi.NewAPI(http.NewServeMux(), httpapi.APIDeps{}).OpenAPI()

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")

			if err := enc.Encode(spec); err != nil {
				return fmt.Errorf("print openapi spec: %w", err)
			}

			return nil
		},
	}
}
