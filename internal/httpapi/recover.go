package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

// internalErrorProblem is the RFC 9457 body answered under /api/ when a handler panics.
const internalErrorProblem = `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`

// panicLogger holds the logger the recovery middleware logs a panic with. The request logging
// middleware, which runs further in, replaces it with the request logger so that the panic is
// logged with the request id.
type panicLogger struct {
	logger *slog.Logger
}

type panicLoggerKey struct{}

// setPanicLogger makes the recovery middleware of the request log panics with logger. It does
// nothing when the recovery middleware did not run.
func setPanicLogger(ctx context.Context, logger *slog.Logger) {
	if p, ok := ctx.Value(panicLoggerKey{}).(*panicLogger); ok {
		p.logger = logger
	}
}

// RecoverMiddleware recovers from panics in the next handlers, so that the server keeps serving.
// It logs the panic value and stack trace at error level and answers 500: a problem under /api/,
// plain text elsewhere. When the response has already started its status can no longer change,
// so the response is aborted instead. http.ErrAbortHandler is propagated for net/http to abort
// the response silently.
func RecoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pl := &panicLogger{logger: logger}
			rec := &responseRecorder{ResponseWriter: w}

			defer func() {
				v := recover()
				if v == nil {
					return
				}

				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(http.ErrAbortHandler)
				}

				pl.logger.ErrorContext(r.Context(), "panic serving request",
					slog.Any("panic", v), slog.String("stack", string(debug.Stack())))

				if rec.started() {
					panic(http.ErrAbortHandler)
				}

				writeInternalError(w, r)
			}()

			next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), panicLoggerKey{}, pl)))
		})
	}
}

// writeInternalError answers 500 in place of the response the handler meant to send, dropping the
// headers that described it or would have changed client state.
func writeInternalError(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{"Cache-Control", "Content-Encoding", "Etag", "Last-Modified", "Set-Cookie"} {
		w.Header().Del(name)
	}

	if !strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(w, internalErrorProblem)
}
