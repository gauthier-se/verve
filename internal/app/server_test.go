package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// syncBuffer is a bytes.Buffer safe for concurrent writes by a logger and reads by a test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func testLogger() (*slog.Logger, *syncBuffer) {
	var buf syncBuffer

	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

// runningServer is a Server started by startServer.
type runningServer struct {
	addr   string
	cancel context.CancelFunc
	done   <-chan error
}

// startServer runs a server on a random local port and waits until it listens.
func startServer(t *testing.T, cfg ServerConfig, h http.Handler, logger *slog.Logger) runningServer {
	t.Helper()

	cfg.Addr = "127.0.0.1:0"
	srv := NewServer(cfg, h, logger)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- srv.Run(ctx) }()

	select {
	case addr := <-srv.Ready():
		return runningServer{addr: addr.String(), cancel: cancel, done: done}
	case err := <-done:
		cancel()
		t.Fatalf("server stopped before listening: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("server did not start listening")
	}

	return runningServer{}
}

// wait returns the result of Run.
func (s runningServer) wait(t *testing.T) error {
	t.Helper()

	select {
	case err := <-s.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")

		return nil
	}
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	return http.DefaultClient.Do(req)
}

// Given a valid configuration with port 0, then the server listens and logs the bound address
// with the actual port.
func TestServer_ServesOnRandomPort(t *testing.T) {
	t.Parallel()

	logger, logs := testLogger()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	srv := startServer(t, ServerConfig{}, h, logger)

	_, port, err := net.SplitHostPort(srv.addr)
	require.NoError(t, err)
	require.NotEqual(t, "0", port)
	require.Contains(t, logs.String(), "addr="+srv.addr)

	res, err := get(t.Context(), "http://"+srv.addr+"/")
	require.NoError(t, err)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "ok", string(body))

	srv.cancel()
	require.NoError(t, srv.wait(t))
}

// Given a request in flight that takes 500 ms, when the server receives SIGTERM (context
// cancelled), then the request completes with 200, new connections are refused and Run
// returns nil.
func TestServer_GracefulShutdownWaitsForInFlight(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	logger, _ := testLogger()
	srv := startServer(t, ServerConfig{}, h, logger)

	type response struct {
		status int
		err    error
	}

	responses := make(chan response, 1)

	go func() {
		res, err := get(context.Background(), "http://"+srv.addr+"/slow")
		if err != nil {
			responses <- response{err: err}

			return
		}

		_ = res.Body.Close()
		responses <- response{status: res.StatusCode}
	}()

	<-started
	srv.cancel()

	require.Eventually(t, func() bool {
		var d net.Dialer

		conn, err := d.DialContext(t.Context(), "tcp", srv.addr)
		if err != nil {
			return true
		}

		_ = conn.Close()

		return false
	}, 400*time.Millisecond, 10*time.Millisecond, "new connections must be refused during shutdown")

	res := <-responses
	require.NoError(t, res.err)
	require.Equal(t, http.StatusOK, res.status)
	require.NoError(t, srv.wait(t))
}

// Given a request still running when the shutdown timeout expires, then connections are
// closed, a warning is logged and Run returns nil.
func TestServer_ShutdownTimeoutForcesClose(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	})

	logger, logs := testLogger()
	srv := startServer(t, ServerConfig{ShutdownTimeout: 100 * time.Millisecond}, h, logger)

	clientErr := make(chan error, 1)

	go func() {
		res, err := get(context.Background(), "http://"+srv.addr+"/stuck")
		if err == nil {
			_ = res.Body.Close()
		}
		clientErr <- err
	}()

	<-started
	srv.cancel()

	require.NoError(t, srv.wait(t))
	require.Contains(t, logs.String(), `level=WARN msg="shutdown timeout expired`)

	select {
	case err := <-clientErr:
		require.Error(t, err, "the connection must be closed before the request completes")
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was not closed")
	}
}

// Given the listen port is already in use, then Run fails with an error naming the address.
func TestServer_PortInUse(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	taken, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = taken.Close() })

	addr := taken.Addr().String()
	logger, _ := testLogger()
	srv := NewServer(ServerConfig{Addr: addr}, http.NotFoundHandler(), logger)

	err = srv.Run(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), addr)
}
