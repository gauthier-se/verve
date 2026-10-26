// Package httpapi builds the HTTP handler of the server.
package httpapi

import (
	"log/slog"
	"net/http"
	"slices"
)

// Deps holds what the handler needs. It grows as routes are added.
type Deps struct {
	Logger *slog.Logger
}

// NewHandler returns the root handler of the server. It has no routes yet and answers 404.
func NewHandler(_ Deps) http.Handler {
	mux := http.NewServeMux()

	return chain(mux)
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
