package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/gauthier-se/verve/internal/httpapi"
)

// startingProblem is the RFC 9457 body answered on the application routes while the server starts.
const startingProblem = `{"type":"about:blank","title":"Service Unavailable","status":503,"code":"starting"}`

// readiness is the startup state of the server: starting with a reason, then ready.
type readiness struct {
	// reason says why the server is starting, nil once it is ready.
	reason atomic.Pointer[string]
	// ready are the readiness checks of the running server.
	ready []httpapi.Checker
}

// newReadiness returns the state of a server starting for reason, whose readiness checks are
// checkers once it is ready.
func newReadiness(reason string, checkers ...httpapi.Checker) *readiness {
	r := &readiness{ready: checkers}
	r.reason.Store(&reason)

	return r
}

// markReady ends the startup.
func (r *readiness) markReady() {
	r.reason.Store(nil)
}

// checkers returns the readiness checks: while starting, only the startup, failing with its
// reason; once ready, the checks of the running server.
func (r *readiness) checkers() []httpapi.Checker {
	if reason := r.reason.Load(); reason != nil {
		return []httpapi.Checker{startupChecker(*reason)}
	}

	return r.ready
}

// gate answers the requests other than the health probes with a 503 problem with the code
// "starting" while the server starts, and lets every request through once it is ready.
func (r *readiness) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.reason.Load() != nil && !httpapi.IsHealthProbe(req.URL.Path) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, startingProblem)

			return
		}

		next.ServeHTTP(w, req)
	})
}

// startupChecker is the readiness check of a starting server: it fails with the reason.
type startupChecker string

func (startupChecker) Name() string {
	return "starting"
}

func (c startupChecker) Check(context.Context) error {
	return errors.New(string(c))
}
