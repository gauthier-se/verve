// Package api is Verve's HTTP layer: a small net/http server that exposes the
// query engine as a JSON API returning only server-side aggregated buckets,
// never a raw series (ADR 0012). Every request is scoped to one Account (ADR
// 0007); until auth lands (slice 05) the target Account comes from a dev flag
// or the X-Verve-Account header.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// accountHeader lets a request override the server's dev Account, so a single
// `verve serve` can be exercised as different Accounts before auth exists.
const accountHeader = "X-Verve-Account"

// errNoAccount means the request named no Account and the server has no dev
// default to fall back on.
var errNoAccount = errors.New("no account: set the -account flag or send an " + accountHeader + " header")

// Server holds the HTTP layer's dependencies. It owns no global state.
type Server struct {
	logger     *slog.Logger
	models     data.Models
	engine     query.Engine
	devAccount string // default Account email until auth (slice 05); may be empty
}

// New builds a Server. devAccount is the fallback Account email a request is
// scoped to when it sends no X-Verve-Account header; it may be empty, in which
// case every request must carry the header.
func New(logger *slog.Logger, models data.Models, engine query.Engine, devAccount string) *Server {
	return &Server{logger: logger, models: models, engine: engine, devAccount: devAccount}
}

// Handler builds the routed, panic-recovering http.Handler for the server. It
// uses Go 1.22 method+pattern routing so an unknown path 404s and a wrong
// method 405s for free.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/metrics", s.handleMetrics)
	mux.HandleFunc("GET /v1/series", s.handleSeries)
	// The mux's method+pattern routing 404s an unknown path and 405s a known
	// path with the wrong method for free; those routing-level errors keep the
	// stdlib's plain-text body, while every application error below is JSON.
	return s.recoverPanic(mux)
}

// accountForRequest resolves the owning Account for a request: the
// X-Verve-Account header if present, else the server's dev default. It returns
// errNoAccount when neither is set and data.ErrRecordNotFound when the named
// Account does not exist.
func (s *Server) accountForRequest(ctx context.Context, r *http.Request) (int64, error) {
	email := r.Header.Get(accountHeader)
	if email == "" {
		email = s.devAccount
	}
	if email == "" {
		return 0, errNoAccount
	}
	acc, err := s.models.Accounts.GetByEmail(ctx, email)
	if err != nil {
		return 0, err
	}
	return acc.ID, nil
}
