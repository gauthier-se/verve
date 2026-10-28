package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/httpapi"
)

// wantStartingProblem is the problem answered on the application routes while the server starts.
const wantStartingProblem = `{"type":"about:blank","title":"Service Unavailable","status":503,"code":"starting"}`

// stubChecker is a readiness check with a fixed result.
type stubChecker struct {
	name string
	err  error
}

func (c stubChecker) Name() string {
	return c.name
}

func (c stubChecker) Check(context.Context) error {
	return c.err
}

// gated returns the gate of r in front of a handler answering 200 "routed".
func gated(r *readiness) http.Handler {
	return r.gate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "routed")
	}))
}

func serveGated(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))

	return rec
}

// Given the server starting, then the gate answers every application route with a 503 problem
// with the code "starting".
func TestReadiness_GateAnswersStartingOnApplicationRoutes(t *testing.T) {
	t.Parallel()

	h := gated(newReadiness())

	for _, req := range []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/workouts/123"},
		{http.MethodGet, "/api/v1/anything"},
		{http.MethodPost, "/api/v1/anything"},
		{http.MethodGet, "/healthz/"},
	} {
		rec := serveGated(t, h, req.method, req.target)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, req)
		require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"), req)
		require.JSONEq(t, wantStartingProblem, rec.Body.String(), req)
	}
}

// Given the server starting, then the gate lets the health probes through.
func TestReadiness_GateLetsHealthProbesThrough(t *testing.T) {
	t.Parallel()

	h := gated(newReadiness())

	for _, target := range []string{"/healthz", "/readyz"} {
		rec := serveGated(t, h, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		require.Equal(t, "routed", rec.Body.String(), target)
	}
}

// Given the server marked ready, then the gate lets every request through.
func TestReadiness_ReadyGateLetsEverythingThrough(t *testing.T) {
	t.Parallel()

	r := newReadiness()
	r.markReady()

	h := gated(r)

	for _, target := range []string{"/", "/api/v1/anything", "/healthz"} {
		rec := serveGated(t, h, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		require.Equal(t, "routed", rec.Body.String(), target)
	}
}

// Given the server starting, then the readiness checks are the startup alone, failing with its
// reason; once ready, they are the checks of the running server.
func TestReadiness_CheckersReportStartingUntilReady(t *testing.T) {
	t.Parallel()

	database := stubChecker{name: "database"}
	dataDir := stubChecker{name: "data_dir"}
	r := newReadiness(database, dataDir)

	starting := r.checkers()
	require.Len(t, starting, 1)
	require.Equal(t, "starting", starting[0].Name())
	require.EqualError(t, starting[0].Check(t.Context()), "applying migrations")

	r.markReady()

	require.Equal(t, []httpapi.Checker{database, dataDir}, r.checkers())
}
