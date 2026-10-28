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
}

// NewHandler returns the root handler of the server. Its only routes are the health probes:
// every other request gets a 404.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(d.Version, d.Checkers))

	return newHandler(d, mux)
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
