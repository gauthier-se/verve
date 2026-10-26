package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Given no routes yet, then every request gets a 404.
func TestNewHandler_UnknownPathReturns404(t *testing.T) {
	t.Parallel()

	h := NewHandler(Deps{Logger: slog.New(slog.DiscardHandler)})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/anything", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
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
