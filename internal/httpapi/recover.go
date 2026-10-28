package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

// internalErrorProblem is the RFC 9457 body answered under /api/ when a handler panics.
const internalErrorProblem = `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`

// RecoverMiddleware recovers from panics in the next handlers, so that the server keeps serving,
// and handles them with answerPanic. It is the safety net of the middlewares in front of
// RequestLogMiddleware, which recovers the panics behind it itself to log them with the request id.
func RecoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sr := &statusRecorder{ResponseWriter: w}

			defer func() {
				if v := recover(); v != nil {
					answerPanic(v, sr, r, logger)
				}
			}()

			next.ServeHTTP(sr, r)
		})
	}
}

// answerPanic handles the value v recovered from a panic while serving r. It logs the value and
// the stack trace at error level and answers 500: a problem under /api/, plain text elsewhere.
// When the response has already started, its status can no longer change: answerPanic aborts it
// by panicking with http.ErrAbortHandler. It propagates http.ErrAbortHandler without logging it,
// for net/http to abort the response silently.
func answerPanic(v any, w *statusRecorder, r *http.Request, logger *slog.Logger) {
	if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(http.ErrAbortHandler)
	}

	logger.ErrorContext(r.Context(), "panic serving request",
		slog.Any("panic", v), slog.String("stack", string(debug.Stack())))

	if w.started() {
		panic(http.ErrAbortHandler)
	}

	writeInternalError(w, r)
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
