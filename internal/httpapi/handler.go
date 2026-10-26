// Package httpapi builds the HTTP handler of the server.
package httpapi

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
)

// Deps holds what the handler needs. It grows as routes are added.
type Deps struct {
	Logger         *slog.Logger
	Clock          clock.Clock
	TrustedProxies config.Prefixes
}

// NewHandler returns the root handler of the server. It has no routes yet and answers 404.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()

	return newHandler(d, mux)
}

// newHandler wraps routes with the middleware chain. Order, outermost first: recovery, client
// info, request logging, security headers, router. Middlewares added later keep this order.
func newHandler(d Deps, routes http.Handler) http.Handler {
	return chain(routes,
		ClientInfoMiddleware(d.TrustedProxies, d.Logger, d.Clock),
	)
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
