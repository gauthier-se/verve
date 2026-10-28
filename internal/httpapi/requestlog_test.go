package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
)

// jsonLogs collects the output of a JSON logger. It is safe for concurrent use.
type jsonLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *jsonLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

// lines decodes every line logged so far.
func (l *jsonLogs) lines(t *testing.T) []map[string]any {
	t.Helper()

	l.mu.Lock()
	defer l.mu.Unlock()

	var lines []map[string]any

	for line := range strings.Lines(l.buf.String()) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "line %q", line)

		lines = append(lines, m)
	}

	return lines
}

// withMessage returns the lines logged with msg.
func (l *jsonLogs) withMessage(t *testing.T, msg string) []map[string]any {
	t.Helper()

	var lines []map[string]any

	for _, line := range l.lines(t) {
		if line["msg"] == msg {
			lines = append(lines, line)
		}
	}

	return lines
}

// requestLines returns the request log lines.
func (l *jsonLogs) requestLines(t *testing.T) []map[string]any {
	t.Helper()

	return l.withMessage(t, "http request")
}

// testServer is the full middleware chain around routes, logging JSON at level into logs.
type testServer struct {
	handler http.Handler
	logs    *jsonLogs
	clock   *clock.Fake
}

func newTestServer(t *testing.T, level string, routes func(mux *http.ServeMux, clk *clock.Fake)) testServer {
	t.Helper()

	logs := &jsonLogs{}
	clk := clock.NewFake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mux := http.NewServeMux()
	routes(mux, clk)

	h := newHandler(Deps{
		Logger:         logging.New(logs, config.LogConfig{Level: level, Format: "json"}),
		Clock:          clk,
		TrustedProxies: prefixes(t, "10.0.0.0/8"),
	}, mux)

	return testServer{handler: h, logs: logs, clock: clk}
}

// do sends a request from peer and returns the response.
func (s testServer) do(t *testing.T, method, target, peer string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	r.RemoteAddr = peer

	for name, values := range header {
		r.Header[name] = values
	}

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)

	return rec
}

const (
	untrustedPeer = "203.0.113.5:4321"
	trustedPeer   = "10.0.0.2:4321"
)

// Given an HTTP request, then one info line is logged with request_id, method, route, path
// without query string, status, duration_ms, bytes and client_ip.
func TestRequestLog_LogsOneInfoLinePerRequest(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "debug", func(mux *http.ServeMux, clk *clock.Fake) {
		mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
			clk.Advance(1500 * time.Microsecond)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "hello")
		})
	})

	rec := srv.do(t, http.MethodGet, "/api/v1/meta?token=abc", untrustedPeer, nil)

	lines := srv.logs.lines(t)
	require.Len(t, lines, 1)

	line := lines[0]
	require.Equal(t, "INFO", line["level"])
	require.Equal(t, "http request", line["msg"])
	require.Equal(t, rec.Header().Get("X-Request-Id"), line["request_id"])
	require.Equal(t, "GET", line["method"])
	require.Equal(t, "GET /api/v1/meta", line["route"])
	require.Equal(t, "/api/v1/meta", line["path"])
	require.InDelta(t, http.StatusCreated, line["status"], 0)
	require.InDelta(t, 1.5, line["duration_ms"], 0)
	require.InDelta(t, 5, line["bytes"], 0)
	require.Equal(t, "203.0.113.5", line["client_ip"])
}

// Given a request that matches no route, then the route is logged as unmatched.
func TestRequestLog_UnmatchedRoute(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /api/v1/meta", func(http.ResponseWriter, *http.Request) {})
	})

	srv.do(t, http.MethodGet, "/nope", untrustedPeer, nil)
	srv.do(t, http.MethodPost, "/api/v1/meta", untrustedPeer, nil)

	lines := srv.logs.requestLines(t)
	require.Len(t, lines, 2)
	require.Equal(t, "unmatched", lines[0]["route"])
	require.InDelta(t, http.StatusNotFound, lines[0]["status"], 0)
	require.Equal(t, "unmatched", lines[1]["route"])
	require.InDelta(t, http.StatusMethodNotAllowed, lines[1]["status"], 0)
}

// Given requests to /healthz or /readyz, then they are logged at debug level only.
func TestRequestLog_HealthEndpointsAtDebug(t *testing.T) {
	t.Parallel()

	routes := func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
		mux.HandleFunc("GET /readyz", func(http.ResponseWriter, *http.Request) {})
	}

	debug := newTestServer(t, "debug", routes)
	debug.do(t, http.MethodGet, "/healthz", untrustedPeer, nil)
	debug.do(t, http.MethodGet, "/readyz", untrustedPeer, nil)

	lines := debug.logs.requestLines(t)
	require.Len(t, lines, 2)
	require.Equal(t, "DEBUG", lines[0]["level"])
	require.Equal(t, "/healthz", lines[0]["path"])
	require.Equal(t, "DEBUG", lines[1]["level"])
	require.Equal(t, "/readyz", lines[1]["path"])

	info := newTestServer(t, "info", routes)
	info.do(t, http.MethodGet, "/healthz", untrustedPeer, nil)
	info.do(t, http.MethodGet, "/readyz", untrustedPeer, nil)

	require.Empty(t, info.logs.lines(t))
}

// Given a handler that never calls WriteHeader, then the logged status is 200.
func TestRequestLog_DefaultStatusIs200(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /empty", func(http.ResponseWriter, *http.Request) {})
		mux.HandleFunc("GET /body", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "body")
		})
	})

	srv.do(t, http.MethodGet, "/empty", untrustedPeer, nil)
	srv.do(t, http.MethodGet, "/body", untrustedPeer, nil)

	lines := srv.logs.requestLines(t)
	require.Len(t, lines, 2)
	require.InDelta(t, http.StatusOK, lines[0]["status"], 0)
	require.InDelta(t, 0, lines[0]["bytes"], 0)
	require.InDelta(t, http.StatusOK, lines[1]["status"], 0)
	require.InDelta(t, 4, lines[1]["bytes"], 0)
}

// Given informational headers before the final one, then the final status is logged.
func TestRequestLog_LogsFinalStatusAfterInformational(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /hints", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusNoContent)
		})
	})

	srv.do(t, http.MethodGet, "/hints", untrustedPeer, nil)

	require.InDelta(t, http.StatusNoContent, srv.logs.requestLines(t)[0]["status"], 0)
}

// Given a request without X-Request-Id, then a random 16-byte hex id is generated and returned
// in the X-Request-Id response header.
func TestRequestLog_GeneratesRequestID(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(*http.ServeMux, *clock.Fake) {})

	first := srv.do(t, http.MethodGet, "/", untrustedPeer, nil).Header().Get("X-Request-Id")
	second := srv.do(t, http.MethodGet, "/", untrustedPeer, nil).Header().Get("X-Request-Id")

	require.Regexp(t, `^[0-9a-f]{32}$`, first)
	require.Regexp(t, `^[0-9a-f]{32}$`, second)
	require.NotEqual(t, first, second)

	lines := srv.logs.requestLines(t)
	require.Equal(t, first, lines[0]["request_id"])
	require.Equal(t, second, lines[1]["request_id"])
}

// Given an incoming X-Request-Id, then it is reused only from a trusted proxy and with a valid
// format; otherwise a new id is generated.
func TestRequestLog_ReusesRequestIDFromTrustedProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		peer     string
		incoming string
		reused   bool
	}{
		{name: "trusted proxy, letters digits dot underscore hyphen", peer: trustedPeer, incoming: "req-1_A.b", reused: true},
		{name: "trusted proxy, equals sign", peer: trustedPeer, incoming: "Root=1-abc", reused: false},
		{name: "trusted proxy, 64 characters", peer: trustedPeer, incoming: strings.Repeat("a", 64), reused: true},
		{name: "trusted proxy, 65 characters", peer: trustedPeer, incoming: strings.Repeat("a", 65), reused: false},
		{name: "trusted proxy, space", peer: trustedPeer, incoming: "req 1", reused: false},
		{name: "trusted proxy, newline", peer: trustedPeer, incoming: "req1\nlevel=ERROR", reused: false},
		{name: "trusted proxy, empty", peer: trustedPeer, incoming: "", reused: false},
		{name: "untrusted peer, valid id", peer: untrustedPeer, incoming: "req-1_A.b", reused: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t, "info", func(*http.ServeMux, *clock.Fake) {})

			rec := srv.do(t, http.MethodGet, "/", tt.peer, http.Header{"X-Request-Id": {tt.incoming}})

			got := rec.Header().Get("X-Request-Id")
			require.Equal(t, got, srv.logs.requestLines(t)[0]["request_id"])

			if tt.reused {
				require.Equal(t, tt.incoming, got)
			} else {
				require.Regexp(t, `^[0-9a-f]{32}$`, got)
			}
		})
	}
}

// Given a handler calling logging.FromContext(ctx).Info, then the line includes the request_id.
func TestRequestLog_StoresRequestLoggerInContext(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /work", func(_ http.ResponseWriter, r *http.Request) {
			logging.FromContext(r.Context()).Info("doing work", slog.Int("items", 3))
		})
	})

	rec := srv.do(t, http.MethodGet, "/work", untrustedPeer, nil)

	lines := srv.logs.withMessage(t, "doing work")
	require.Len(t, lines, 1)
	require.Equal(t, rec.Header().Get("X-Request-Id"), lines[0]["request_id"])
	require.InDelta(t, 3, lines[0]["items"], 0)
}

// Given a handler using http.ResponseController, then flushing reaches the underlying writer
// through the logging wrapper.
func TestRequestLog_KeepsResponseControllerWorking(t *testing.T) {
	t.Parallel()

	var flushErr error

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /stream", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "chunk")
			flushErr = http.NewResponseController(w).Flush()
		})
	})

	rec := srv.do(t, http.MethodGet, "/stream", untrustedPeer, nil)

	require.NoError(t, flushErr)
	require.True(t, rec.Flushed)
}
