package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/version"
)

// fakeChecker is a readiness check with a fixed result.
type fakeChecker struct {
	name string
	err  error
}

func (c fakeChecker) Name() string {
	return c.name
}

func (c fakeChecker) Check(context.Context) error {
	return c.err
}

// healthHandler returns the handler of a server at version 1.2.3 whose readiness checks are checkers.
func healthHandler(checkers ...Checker) http.Handler {
	return NewHandler(Deps{
		Logger:   slog.New(slog.DiscardHandler),
		Clock:    clock.Real(),
		Version:  version.Info{Version: "1.2.3", Commit: "abc1234", BuildDate: "2026-10-28T10:00:00Z"},
		Checkers: func() []Checker { return checkers },
	})
}

func serve(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))

	return rec
}

// Given the server is running, then GET and HEAD /healthz return 200 {"status":"ok"}, whatever
// the readiness checks say.
func TestHealthz_ReturnsOK(t *testing.T) {
	t.Parallel()

	h := healthHandler(fakeChecker{name: "database", err: errors.New("unreachable")})

	rec := serve(t, h, http.MethodGet, "/healthz")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())

	rec = serve(t, h, http.MethodHead, "/healthz")
	require.Equal(t, http.StatusOK, rec.Code)
}

// Given every readiness check passing, then GET /readyz returns 200 with the version.
func TestReadyz_AllChecksPassReturnsReady(t *testing.T) {
	t.Parallel()

	h := healthHandler(fakeChecker{name: "database"}, fakeChecker{name: "migrations"}, fakeChecker{name: "data_dir"})

	rec := serve(t, h, http.MethodGet, "/readyz")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"status":"ready","version":"1.2.3"}`, rec.Body.String())
}

// Given failing readiness checks, then GET /readyz returns 503 with the version and one reason
// per failing check, prefixed with its name.
func TestReadyz_FailingChecksReturnNotReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		checkers []Checker
		want     string
	}{
		{
			name: "one failing",
			checkers: []Checker{
				fakeChecker{name: "database"},
				fakeChecker{name: "migrations", err: errors.New("2 pending")},
				fakeChecker{name: "data_dir"},
			},
			want: `{"status":"not_ready","version":"1.2.3","reasons":["migrations: 2 pending"]}`,
		},
		{
			name: "several failing",
			checkers: []Checker{
				fakeChecker{name: "database", err: errors.New("ping writer: sql: database is closed")},
				fakeChecker{name: "migrations"},
				fakeChecker{name: "data_dir", err: errors.New("not writable")},
			},
			want: `{"status":"not_ready","version":"1.2.3","reasons":` +
				`["database: ping writer: sql: database is closed","data_dir: not writable"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, healthHandler(tt.checkers...), http.MethodGet, "/readyz")
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			require.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

// Given /healthz and /readyz, then IsHealthProbe recognizes them and nothing else.
func TestIsHealthProbe(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/healthz", "/readyz"} {
		require.True(t, IsHealthProbe(path), path)
	}

	for _, path := range []string{"/", "/healthz/", "/api/v1/healthz", "/readyz/x", "/metrics"} {
		require.False(t, IsHealthProbe(path), path)
	}
}
