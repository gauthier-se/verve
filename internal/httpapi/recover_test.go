package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

// panickingRoutes registers handlers that panic with "boom" under /api/ and elsewhere, and a
// handler that succeeds.
func panickingRoutes(mux *http.ServeMux, _ *clock.Fake) {
	boom := func(http.ResponseWriter, *http.Request) { panic("boom") }

	mux.HandleFunc("GET /api/v1/boom", boom)
	mux.HandleFunc("GET /boom", boom)
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
}

// Given a handler under /api/ that panics, then the client receives a 500 problem, and the
// panic value and stack trace are logged at error level with the request id.
func TestRecover_PanicUnderAPIReturnsProblem(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", panickingRoutes)

	rec := srv.do(t, http.MethodGet, "/api/v1/boom", untrustedPeer, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`,
		rec.Body.String())

	requestID := rec.Header().Get("X-Request-Id")
	require.NotEmpty(t, requestID)

	panics := srv.logs.withMessage(t, "panic serving request")
	require.Len(t, panics, 1)
	require.Equal(t, "ERROR", panics[0]["level"])
	require.Equal(t, requestID, panics[0]["request_id"])
	require.Equal(t, "boom", panics[0]["panic"])
	require.Contains(t, panics[0]["stack"], "httpapi.panickingRoutes")
}

// Given a handler outside /api/ that panics, then the client receives a plain text 500.
func TestRecover_PanicElsewhereReturnsPlainText(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", panickingRoutes)

	rec := srv.do(t, http.MethodGet, "/boom", untrustedPeer, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "Internal Server Error\n", rec.Body.String())
	require.Len(t, srv.logs.withMessage(t, "panic serving request"), 1)
}

// Given a handler that panics, then the request is still logged once, with status 500.
func TestRecover_PanickingRequestIsLoggedWithStatus500(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", panickingRoutes)

	rec := srv.do(t, http.MethodGet, "/boom", untrustedPeer, nil)

	lines := srv.logs.requestLines(t)
	require.Len(t, lines, 1)
	require.Equal(t, rec.Header().Get("X-Request-Id"), lines[0]["request_id"])
	require.Equal(t, "GET /boom", lines[0]["route"])
	require.InDelta(t, http.StatusInternalServerError, lines[0]["status"], 0)
}

// Given a handler that panics, then the server keeps serving the next requests.
func TestRecover_ServerKeepsServing(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", panickingRoutes)
	ts := httptest.NewServer(srv.handler)
	t.Cleanup(ts.Close)

	get := func(path string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
		require.NoError(t, err)

		resp, err := ts.Client().Do(req)
		require.NoError(t, err)

		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		return resp.StatusCode, string(body)
	}

	status, _ := get("/boom")
	require.Equal(t, http.StatusInternalServerError, status)

	status, body := get("/ok")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "ok", body)
}

// Given a handler that panics with http.ErrAbortHandler, then the panic is propagated so that
// net/http aborts the response silently, and nothing is logged as an error.
func TestRecover_RepanicsErrAbortHandler(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	})

	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		srv.do(t, http.MethodGet, "/abort", untrustedPeer, nil)
	})
	require.Empty(t, srv.logs.withMessage(t, "panic serving request"))
}

// Given a handler that sets response headers then panics, then the 500 drops the headers that
// describe the response it meant to send, and keeps the request id.
func TestRecover_DropsHandlerResponseHeaders(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /api/v1/download", func(w http.ResponseWriter, _ *http.Request) {
			h := w.Header()
			h.Set("Content-Encoding", "gzip")
			h.Set("Cache-Control", "public, max-age=3600")
			h.Set("Etag", `"abc"`)
			h.Set("Last-Modified", "Wed, 01 Jan 2026 12:00:00 GMT")
			h.Set("Set-Cookie", "session=abc")

			panic("boom")
		})
	})

	rec := srv.do(t, http.MethodGet, "/api/v1/download", untrustedPeer, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	for _, name := range []string{"Content-Encoding", "Cache-Control", "Etag", "Last-Modified", "Set-Cookie"} {
		require.Empty(t, rec.Header().Values(name), name)
	}

	require.NotEmpty(t, rec.Header().Get("X-Request-Id"))
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

// Given a handler that panics after the response has started, then the panic is logged and the
// response is aborted, because the status can no longer be changed to 500.
func TestRecover_PanicAfterResponseStartedAborts(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /partial", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "partial")

			panic("boom")
		})
	})

	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		srv.do(t, http.MethodGet, "/partial", untrustedPeer, nil)
	})
	require.Len(t, srv.logs.withMessage(t, "panic serving request"), 1)
}

// Given a handler that flushes through http.ResponseController then panics, then the response
// counts as started: it is aborted, and the request is logged with the status sent, 200.
func TestRecover_PanicAfterFlushAborts(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, "info", func(mux *http.ServeMux, _ *clock.Fake) {
		mux.HandleFunc("GET /stream", func(w http.ResponseWriter, _ *http.Request) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				panic(err)
			}

			panic("boom")
		})
	})

	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		srv.do(t, http.MethodGet, "/stream", untrustedPeer, nil)
	})
	require.Len(t, srv.logs.withMessage(t, "panic serving request"), 1)
	require.InDelta(t, http.StatusOK, srv.logs.requestLines(t)[0]["status"], 0)
}
