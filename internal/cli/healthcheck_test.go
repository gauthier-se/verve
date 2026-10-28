package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// healthServer starts a local server answering every request with status and returns its port.
// It sends the requested paths to paths when not nil.
func healthServer(t *testing.T, status int, paths chan<- string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if paths != nil {
			paths <- r.URL.Path
		}

		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)

	return port
}

func listenAddrEnv(addr string) map[string]string {
	return map[string]string{"VERVE_LISTEN_ADDR": addr}
}

// Given `verve healthcheck` and a healthy server on the configured listen address, then it calls
// /healthz and exits with status 0 without output.
func TestHealthcheck_HealthyServerExitsZero(t *testing.T) {
	t.Parallel()

	paths := make(chan string, 1)
	port := healthServer(t, http.StatusOK, paths)

	res := run(t, depsWithEnv(listenAddrEnv("127.0.0.1:"+port)), "healthcheck")

	require.Equal(t, 0, res.code, res.stderr)
	require.Empty(t, res.stdout)
	require.Empty(t, res.stderr)
	require.Equal(t, "/healthz", <-paths)
}

// Given a server answering 503, then healthcheck exits with status 1 and the status on stderr.
func TestHealthcheck_UnhealthyServerExitsOne(t *testing.T) {
	t.Parallel()

	port := healthServer(t, http.StatusServiceUnavailable, nil)

	res := run(t, depsWithEnv(listenAddrEnv("127.0.0.1:"+port)), "healthcheck")

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr, "Error: healthcheck")
	require.Contains(t, res.stderr, "503 Service Unavailable")
}

// Given no server on the configured listen address, then healthcheck exits with status 1 and the
// connection error on stderr.
func TestHealthcheck_ConnectionRefusedExitsOne(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	res := run(t, depsWithEnv(listenAddrEnv(addr)), "healthcheck")

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr, "Error: healthcheck")
	require.Contains(t, res.stderr, "connection refused")
}

// Given a server that does not answer within the timeout, then healthcheck exits with status 1
// and the timeout on stderr.
func TestHealthcheck_NoAnswerExitsOne(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	deps := depsWithEnv(listenAddrEnv(srv.Listener.Addr().String()))
	deps.HealthcheckTimeout = 50 * time.Millisecond

	res := run(t, deps, "healthcheck")

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr, "Error: healthcheck")
	require.Contains(t, res.stderr, "Client.Timeout exceeded")
}

// Given a listen address without host or with an unspecified host, then healthcheck targets the
// loopback address on that port.
func TestHealthcheck_UnspecifiedHostTargetsLoopback(t *testing.T) {
	t.Parallel()

	port := healthServer(t, http.StatusOK, nil)

	for _, addr := range []string{":" + port, "0.0.0.0:" + port, "[::]:" + port} {
		res := run(t, depsWithEnv(listenAddrEnv(addr)), "healthcheck")
		require.Equal(t, 0, res.code, "%s: %s", addr, res.stderr)
	}
}

// Given the listen address in the file passed with --config, then healthcheck targets it.
func TestHealthcheck_HonorsConfigFile(t *testing.T) {
	t.Parallel()

	port := healthServer(t, http.StatusOK, nil)
	path := writeConfigFile(t, "listen_addr: \"127.0.0.1:"+port+"\"\n")

	res := run(t, testDeps(), "healthcheck", "--config", path)

	require.Equal(t, 0, res.code, res.stderr)
}

// Given a listen address that is not host:port, then healthcheck exits with status 1.
func TestHealthcheck_InvalidListenAddrExitsOne(t *testing.T) {
	t.Parallel()

	res := run(t, depsWithEnv(listenAddrEnv("8080")), "healthcheck")

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr, `Error: healthcheck: invalid listen address "8080"`)
}

// Given a server that never answers and no timeout configured, then healthcheck gives up after
// 3 seconds with status 1.
func TestHealthcheck_DefaultTimeoutIsThreeSeconds(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	start := time.Now()
	res := run(t, depsWithEnv(listenAddrEnv(srv.Listener.Addr().String())), "healthcheck")
	elapsed := time.Since(start)

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr, "Client.Timeout exceeded")
	require.GreaterOrEqual(t, elapsed, 3*time.Second)
	require.Less(t, elapsed, 10*time.Second)
}
