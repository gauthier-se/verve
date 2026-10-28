// Package httpapi builds the HTTP handler of the server.
package httpapi

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// Deps holds what the handler needs. It grows as routes are added.
type Deps struct {
	Logger         *slog.Logger
	Clock          clock.Clock
	TrustedProxies config.Prefixes
	Store          store.Store
	// Version is the version of the running binary, reported by /readyz.
	Version string
	// Checkers returns the readiness checks of /readyz, called on each request. Nil means none.
	Checkers func() []Checker
	// Gate, when not nil, wraps the router, behind the other middlewares. The server answers the
	// requests there while it starts.
	Gate func(http.Handler) http.Handler
	// SPA serves the web app. Nil means none.
	SPA http.Handler
}

// NewHandler returns the root handler of the server: the health probes, a JSON 404 problem for
// any unknown path under /api/, and the web app for every other path.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(d.Version, d.Checkers))
	mux.HandleFunc("/api/", apiNotFound)

	if d.SPA != nil {
		mux.Handle("/", d.SPA)
	}

	return newHandler(d, mux)
}

// apiNotFound answers the unknown paths under /api/, whatever the method.
func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	WriteProblem(w, Problem{Status: http.StatusNotFound, Code: "not_found"})
}

// newHandler wraps routes with the middleware chain. Order, outermost first: recovery, client
// info, request logging, security headers, gate, router. Middlewares added later keep this order.
func newHandler(d Deps, routes http.Handler) http.Handler {
	middlewares := []middleware{
		RecoverMiddleware(d.Logger),
		ClientInfoMiddleware(d.TrustedProxies, d.Logger, d.Clock),
		RequestLogMiddleware(d.Logger, d.Clock),
	}

	if d.Gate != nil {
		middlewares = append(middlewares, d.Gate)
	}

	return chain(routes, middlewares...)
}

// middleware wraps a handler.
type middleware func(http.Handler) http.Handler

// chain wraps h with middlewares, the first one being the outermost.
func chain(h http.Handler, middlewares ...middleware) http.Handler {
	for _, m := range slices.Backward(middlewares) {
		h = m(h)
	}

	return h
}
