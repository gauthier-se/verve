package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/logging"
)

// headerRequestID carries the request id, in requests from a trusted proxy and in every response.
const headerRequestID = "X-Request-Id"

// Lookup tables, never modified.
var (
	// validRequestID matches the incoming request ids that may be reused.
	validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	// probePaths are polled by orchestrators: their requests are logged at debug level.
	probePaths = []string{"/healthz", "/readyz"}
)

// RequestLogMiddleware logs one line per request with its id, method, route pattern, path,
// status, duration, body size and client IP, at info level except for the health probes, logged
// at debug level. It must run after ClientInfoMiddleware.
//
// The request id is the X-Request-Id sent by a trusted proxy when it is well-formed, otherwise a
// new random one. It is returned in the X-Request-Id response header, and the request logger
// carrying it is stored in the context for logging.FromContext.
//
// Panics in the next handlers are recovered and handled with answerPanic, so that they are
// logged with the request id and the request with the status and size of the 500.
func RequestLogMiddleware(logger *slog.Logger, clk clock.Clock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := clk.Now()
			id := requestID(r)
			reqLogger := logger.With(slog.String("request_id", id))

			w.Header().Set(headerRequestID, id)

			sr := &statusRecorder{ResponseWriter: w}
			// The router sets the matched pattern on this request.
			req := r.WithContext(logging.WithContext(r.Context(), reqLogger))
			returned := false

			// Deferred first so that it runs last: after a panic is answered, and even when
			// answerPanic aborts the response.
			defer func() {
				status := sr.finalStatus()
				if !returned && !sr.started() {
					// The response was aborted before anything was sent.
					status = http.StatusInternalServerError
				}

				level := slog.LevelInfo
				if IsHealthProbe(req.URL.Path) {
					level = slog.LevelDebug
				}

				route := req.Pattern
				if route == "" {
					route = "unmatched"
				}

				reqLogger.LogAttrs(req.Context(), level, "http request",
					slog.String("method", req.Method),
					slog.String("route", route),
					slog.String("path", req.URL.Path),
					slog.Int("status", status),
					slog.Float64("duration_ms", float64(clk.Now().Sub(start).Microseconds())/1000),
					slog.Int64("bytes", sr.bytes),
					slog.String("client_ip", ClientInfoFrom(req.Context()).IP.String()),
				)
			}()

			defer func() {
				if v := recover(); v != nil {
					answerPanic(v, sr, req, reqLogger)
				}
			}()

			next.ServeHTTP(sr, req)

			returned = true
		})
	}
}

// requestID returns the X-Request-Id of a request from a trusted proxy when it is well-formed,
// otherwise a new random id: 16 bytes in hex.
func requestID(r *http.Request) string {
	if ClientInfoFrom(r.Context()).ViaTrustedProxy {
		if id := r.Header.Get(headerRequestID); validRequestID.MatchString(id) {
			return id
		}
	}

	var b [16]byte
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(b[:])

	return hex.EncodeToString(b[:])
}
