package app

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/gauthier-se/verve/internal/httpapi"
)

// readiness is the startup state of the server: starting while the migrations are applied, then ready.
type readiness struct {
	ready atomic.Bool
	// readyCheckers are the readiness checks of the server once it is ready.
	readyCheckers []httpapi.Checker
}

// newReadiness returns the state of a starting server whose readiness checks are checkers once
// it is ready.
func newReadiness(checkers ...httpapi.Checker) *readiness {
	return &readiness{readyCheckers: checkers}
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

	return r.readyCheckers
}

// gate answers the requests other than the health probes with a 503 problem with the code
// "starting" while the server starts, and lets every request through once it is ready.
func (r *readiness) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.ready.Load() && !httpapi.IsHealthProbe(req.URL.Path) {
			httpapi.WriteProblem(w, httpapi.Problem{Status: http.StatusServiceUnavailable, Code: "starting"})

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
