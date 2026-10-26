package cli

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/config"
)

// fakeServer records the calls of the server runner instead of starting a server.
type fakeServer struct {
	calls int
	cfg   config.Config
}

func (f *fakeServer) serve(_ context.Context, cfg config.Config, _ *slog.Logger) error {
	f.calls++
	f.cfg = cfg

	return nil
}

func depsWithServer(env map[string]string, srv *fakeServer) Deps {
	deps := depsWithEnv(env)
	deps.Serve = srv.serve

	return deps
}

// Given `verve` with no arguments, then it behaves exactly like `verve serve`.
func TestRoot_NoArgumentsServes(t *testing.T) {
	t.Parallel()

	env := map[string]string{"VERVE_PUBLIC_URL": "https://verve.example.com", "VERVE_LISTEN_ADDR": ":9090"}

	var viaRoot, viaServe fakeServer

	rootRes := run(t, depsWithServer(env, &viaRoot))
	serveRes := run(t, depsWithServer(env, &viaServe), "serve")

	require.Equal(t, 0, rootRes.code, rootRes.stderr)
	require.Equal(t, 1, viaRoot.calls)
	require.Equal(t, ":9090", viaRoot.cfg.ListenAddr)
	require.Equal(t, serveRes, rootRes)
	require.Equal(t, viaServe, viaRoot)
}

// Given `verve serve` with a valid configuration, then the server runner gets the loaded configuration.
func TestServe_ValidConfigRunsServer(t *testing.T) {
	t.Parallel()

	var srv fakeServer

	res := run(t, depsWithServer(map[string]string{"VERVE_DATA_DIR": "/srv/verve"}, &srv), "serve")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, 1, srv.calls)
	require.Equal(t, "/srv/verve", srv.cfg.DataDir)
}

// Given an invalid configuration, then serve exits with status 1, prints the validation
// errors and never starts the server.
func TestServe_InvalidConfigExitsWithoutServing(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"serve"}} {
		var srv fakeServer

		res := run(t, depsWithServer(map[string]string{"VERVE_LOG_LEVEL": "verbose"}, &srv), args...)

		require.Equal(t, 1, res.code)
		require.Contains(t, res.stderr, "invalid configuration")
		require.Contains(t, res.stderr, `log.level (VERVE_LOG_LEVEL): invalid value "verbose"`)
		require.Zero(t, srv.calls)
	}
}
