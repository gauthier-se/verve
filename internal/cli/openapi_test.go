package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireSpec checks that out is an OpenAPI 3.1 document in JSON with the operation get-meta.
func requireSpec(t *testing.T, out string) {
	t.Helper()

	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &spec))
	require.True(t, strings.HasPrefix(spec.OpenAPI, "3.1"), spec.OpenAPI)
	require.Equal(t, "get-meta", spec.Paths["/api/v1/meta"]["get"].OperationID)
}

// Given `verve openapi`, then it prints the OpenAPI 3.1 spec as JSON, the same on every run, so
// that the committed copy can be diffed.
func TestOpenAPICommand_PrintsSpec(t *testing.T) {
	t.Parallel()

	res := run(t, testDeps(), "openapi")

	require.Equal(t, 0, res.code)
	require.Empty(t, res.stderr)
	requireSpec(t, res.stdout)
	require.Equal(t, byte('\n'), res.stdout[len(res.stdout)-1])

	require.Equal(t, res.stdout, run(t, testDeps(), "openapi").stdout)
}

// Given an invalid environment, when I run `verve openapi`, then it still prints the spec: it
// loads no configuration and opens neither the data directory nor the database.
func TestOpenAPICommand_IgnoresConfiguration(t *testing.T) {
	t.Parallel()

	deps := depsWithEnv(map[string]string{"VERVE_LOG_LEVEL": "loud"})

	// The environment is invalid for the commands that load the configuration.
	require.Equal(t, 1, run(t, deps, "config", "check").code)

	res := run(t, deps, "openapi")

	require.Equal(t, 0, res.code)
	require.Empty(t, res.stderr)
	requireSpec(t, res.stdout)
}
