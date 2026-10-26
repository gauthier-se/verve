package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "verve.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// Given a valid configuration, when I run `verve config check`, then it prints the effective
// configuration as YAML, each value annotated with its source, and exits 0.
func TestConfigCheck_ValidPrintsYAMLWithSources(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, "log:\n  level: debug\n")
	deps := depsWithEnv(map[string]string{
		"VERVE_PUBLIC_URL":   "https://verve.example.com",
		"VERVE_METRICS_ADDR": "127.0.0.1:9100",
	})

	res := run(t, deps, "config", "check", "--config", path)

	require.Equal(t, 0, res.code, res.stderr)
	require.Contains(t, res.stdout, "data_dir: /data # default\n")
	require.Contains(t, res.stdout, "public_url: https://verve.example.com # env\n")
	require.Contains(t, res.stdout, "metrics_addr: 127.0.0.1:9100 # env\n")
	require.Contains(t, res.stdout, "  level: debug # file\n")
	require.Empty(t, res.stderr)
}

// Given --config before the subcommand, then the persistent flag still applies.
func TestConfigCheck_ConfigFlagIsPersistent(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, "listen_addr: \":9090\"\n")

	res := run(t, depsWithEnv(map[string]string{"VERVE_PUBLIC_URL": "https://verve.example.com"}),
		"--config", path, "config", "check")

	require.Equal(t, 0, res.code, res.stderr)
	require.Contains(t, res.stdout, "listen_addr: :9090 # file\n")
}

// Given an invalid configuration, when I run `verve config check`, then it prints every
// error and exits 1.
func TestConfigCheck_InvalidPrintsAllErrors(t *testing.T) {
	t.Parallel()

	deps := depsWithEnv(map[string]string{
		"VERVE_TRUSTED_PROXIES": "10.0.0.0/33",
		"VERVE_LOG_LEVEL":       "verbose",
		"VERVE_PUBLIC_URL":      "verve.local",
	})

	res := run(t, deps, "config", "check")

	require.Equal(t, 1, res.code)
	require.Empty(t, res.stdout)
	require.Contains(t, res.stderr, "invalid configuration")
	require.Contains(t, res.stderr, `trusted_proxies (VERVE_TRUSTED_PROXIES): invalid value "10.0.0.0/33"`)
	require.Contains(t, res.stderr, `log.level (VERVE_LOG_LEVEL): invalid value "verbose"`)
	require.Contains(t, res.stderr, `public_url (VERVE_PUBLIC_URL): invalid value "verve.local"`)
	require.NotContains(t, res.stderr, "verve --help")
}

// Given a valid configuration without VERVE_PUBLIC_URL, when I run `verve config check`,
// then it prints the warning on stderr and still exits 0.
func TestConfigCheck_PrintsWarnings(t *testing.T) {
	t.Parallel()

	res := run(t, depsWithEnv(nil), "config", "check")

	require.Equal(t, 0, res.code)
	require.Contains(t, res.stdout, "listen_addr:")
	require.Contains(t, res.stderr, "Warning:")
	require.Contains(t, res.stderr, "VERVE_PUBLIC_URL")
}

// Given VERVE_CONFIG in the environment, when I run `verve config check`, then the file is used.
func TestConfigCheck_VerveConfig(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, "listen_addr: \":9090\"\n")

	res := run(t, depsWithEnv(map[string]string{"VERVE_CONFIG": path}), "config", "check")

	require.Equal(t, 0, res.code, res.stderr)
	require.Contains(t, res.stdout, "listen_addr: :9090 # file\n")
}
