package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
)

// Given no routes yet, then every request gets a 404.
func TestNewHandler_UnknownPathReturns404(t *testing.T) {
	t.Parallel()

	h := NewHandler(Deps{Logger: slog.New(slog.DiscardHandler), Clock: clock.Real()})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/anything", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// Given any handler behind the middleware chain, then ClientInfoFrom returns the resolved information.
func TestNewHandler_ExposesClientInfo(t *testing.T) {
	t.Parallel()

	var got ClientInfo

	h := newHandler(Deps{
		Logger:         slog.New(slog.DiscardHandler),
		Clock:          clock.NewFake(time.Unix(0, 0)),
		TrustedProxies: prefixes(t, "10.0.0.0/8"),
	}, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ClientInfoFrom(r.Context())
	}))

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://internal/", nil)
	r.RemoteAddr = "10.0.0.2:4321"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "verve.example.com")

	h.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, ClientInfo{
		IP:              netip.MustParseAddr("198.51.100.7"),
		Scheme:          "https",
		Host:            "verve.example.com",
		ViaTrustedProxy: true,
	}, got)
}

// Given several middlewares, then the first one is the outermost.
func TestChain_FirstMiddlewareIsOutermost(t *testing.T) {
	t.Parallel()

	var calls []string

	trace := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "handler")
		_, _ = io.WriteString(w, "ok")
	}), trace("outer"), trace("inner"))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	require.Equal(t, []string{"outer", "inner", "handler"}, calls)
	require.Equal(t, "ok", rec.Body.String())
}

// Given a gate, then it wraps the router behind the request logging: the requests it answers
// are logged, and the requests it lets through reach the routes with their pattern logged.
func TestNewHandler_GateWrapsRouter(t *testing.T) {
	t.Parallel()

	logs := &jsonLogs{}
	gate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/closed" {
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			next.ServeHTTP(w, r)
		})
	}

	h := NewHandler(Deps{
		Logger: logging.New(logs, config.LogConfig{Level: "debug", Format: config.LogFormatJSON}),
		Clock:  clock.Real(),
		Gate:   gate,
	})

	require.Equal(t, http.StatusServiceUnavailable, serve(t, h, http.MethodGet, "/closed").Code)
	require.Equal(t, http.StatusOK, serve(t, h, http.MethodGet, "/healthz").Code)

	lines := logs.requestLines(t)
	require.Len(t, lines, 2)
	require.InDelta(t, http.StatusServiceUnavailable, lines[0]["status"], 0)
	require.Equal(t, "unmatched", lines[0]["route"])
	require.InDelta(t, http.StatusOK, lines[1]["status"], 0)
	require.Equal(t, "GET /healthz", lines[1]["route"])
}
