package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/version"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, deps Deps, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := execute(context.Background(), deps, args, &stdout, &stderr)

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func testDeps() Deps {
	return depsWithEnv(nil)
}

// depsWithEnv returns test dependencies whose configuration is loaded from env instead of
// the process environment, so tests can run in parallel.
func depsWithEnv(env map[string]string) Deps {
	return Deps{
		Version: version.Info{Version: "0.1.0", Commit: "abc1234", BuildDate: "2026-10-22T10:00:00Z"},
		Logger:  slog.New(slog.DiscardHandler),
		LoadConfig: func(file string) (config.Config, error) {
			return config.Load(config.LoadOptions{File: file, Env: env})
		},
	}
}

// Given a binary built with version metadata, when I run `verve version`,
// then it prints one line with version, commit and build date.
func TestVersionCommand_PrintsLine(t *testing.T) {
	t.Parallel()

	res := run(t, testDeps(), "version")

	require.Equal(t, 0, res.code)
	require.Equal(t, "verve 0.1.0 (commit abc1234, built 2026-10-22T10:00:00Z)\n", res.stdout)
	require.Empty(t, res.stderr)
}

// Given a binary built without ldflags, when I run `verve version`,
// then it prints the development defaults.
func TestVersionCommand_PrintsDefaults(t *testing.T) {
	t.Parallel()

	res := run(t, Deps{Version: version.Get()}, "version")

	require.Equal(t, 0, res.code)
	require.Equal(t, "verve dev (commit unknown, built unknown)\n", res.stdout)
}

// Given `verve version --json`, then it prints a JSON object with version,
// commit and buildDate followed by a newline.
func TestVersionCommand_JSON(t *testing.T) {
	t.Parallel()

	res := run(t, testDeps(), "version", "--json")

	require.Equal(t, 0, res.code)
	require.JSONEq(t, `{"version":"0.1.0","commit":"abc1234","buildDate":"2026-10-22T10:00:00Z"}`, res.stdout)
	require.Equal(t, byte('\n'), res.stdout[len(res.stdout)-1])

	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &decoded))
	require.Len(t, decoded, 3)
}

// Given `verve --help`, then it shows the binary name, a one-line description
// and the available commands.
func TestRoot_HelpListsCommands(t *testing.T) {
	t.Parallel()

	res := run(t, testDeps(), "--help")

	require.Equal(t, 0, res.code)
	require.Contains(t, res.stdout, "verve")
	require.Contains(t, res.stdout, "Self-hosted health dashboard for Apple Health data")
	require.Contains(t, res.stdout, "Available Commands:")
	require.Contains(t, res.stdout, "version")
	require.Contains(t, res.stdout, "serve")
}

// Given `verve nope`, then it exits with status 1 and prints an error that
// mentions `verve --help`.
func TestRoot_UnknownCommandFails(t *testing.T) {
	t.Parallel()

	res := run(t, testDeps(), "nope")

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr, `unknown command "nope"`)
	require.Contains(t, res.stderr, "verve --help")
	require.Empty(t, res.stdout)
}
