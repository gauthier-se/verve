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

// readiness is the startup state of the server: starting while the migrations are applied, then ready.
type readiness struct {
	ready atomic.Bool
	// running are the readiness checks of the server once it is ready.
	running []httpapi.Checker
}

// newReadiness returns the state of a starting server whose readiness checks are checkers once
// it is ready.
func newReadiness(checkers ...httpapi.Checker) *readiness {
	return &readiness{running: checkers}
}

// markReady ends the startup.
func (r *readiness) markReady() {
	r.ready.Store(true)
}

// checkers returns the readiness checks: while starting, only the startup, failing with its
// reason; once ready, the checks of the running server.
func (r *readiness) checkers() []httpapi.Checker {
	if !r.ready.Load() {
		return []httpapi.Checker{startupChecker{}}
	}

	return r.running
}

// gate answers the requests other than the health probes with a 503 problem with the code
// "starting" while the server starts, and lets every request through once it is ready.
func (r *readiness) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.ready.Load() && !httpapi.IsHealthProbe(req.URL.Path) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, startingProblem)

			return
		}

		next.ServeHTTP(w, req)
	})
}

// startupChecker is the readiness check of a starting server: it fails with the reason.
type startupChecker struct{}

func (startupChecker) Name() string {
	return "starting"
}

func (startupChecker) Check(context.Context) error {
	return errors.New("applying migrations")
}
