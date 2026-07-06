// Package api is Verve's HTTP layer: a small net/http server that exposes the
// query engine as a JSON API returning only server-side aggregated buckets,
// never a raw series (ADR 0012). Every data request is scoped to the
// authenticated Account (ADR 0007): local login sets an opaque session cookie
// (ADR 0008), the authenticate middleware resolves it to an Account, and
// requireAuth rejects unauthenticated access to Account data.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// defaultSessionTTL is how long a login session stays valid absent an override.
const defaultSessionTTL = 30 * 24 * time.Hour

// Config carries the HTTP layer's auth-facing settings.
type Config struct {
	// SecureCookies sets the Secure attribute on the session cookie: true behind
	// HTTPS (production), false for plain-HTTP local development.
	SecureCookies bool
	// SessionTTL is how long a new session lasts; zero uses defaultSessionTTL.
	SessionTTL time.Duration
	// SPA serves the embedded front-end on every non-/v1 path (ADR 0005). It is
	// injected so the api package stays decoupled from the web-assets package and
	// tests can run without embedding a build. Nil means API-only (no SPA mount).
	SPA http.Handler
}

// Server holds the HTTP layer's dependencies. It owns no global state.
type Server struct {
	logger        *slog.Logger
	models        data.Models
	engine        query.Engine
	resolver      authResolver
	loginLimiter  *loginLimiter
	secureCookies bool
	sessionTTL    time.Duration
	spa           http.Handler
	// decoyHash is verified against on logins for missing accounts so timing does
	// not reveal which emails exist. It is a hash of an unguessable value.
	decoyHash string
}

// New builds a Server. cfg tunes cookie security and session lifetime.
func New(logger *slog.Logger, models data.Models, engine query.Engine, cfg Config) *Server {
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	// decoyHash is verified against on logins for missing accounts, so a failed
	// login costs an argon2 verify whether or not the email exists. HashPassword
	// only fails if the RNG does (startup-fatal territory); the empty-string
	// fallback then verifies fast, but that path is effectively unreachable.
	decoy, err := auth.HashPassword("verve-login-timing-decoy")
	if err != nil {
		logger.Error("build login timing decoy", "err", err)
	}

	return &Server{
		logger:        logger,
		models:        models,
		engine:        engine,
		resolver:      sessionResolver{sessions: models.AuthSessions},
		loginLimiter:  newLoginLimiter(),
		secureCookies: cfg.SecureCookies,
		sessionTTL:    ttl,
		spa:           cfg.SPA,
		decoyHash:     decoy,
	}
}

// Handler builds the routed, panic-recovering http.Handler for the server. It
// uses Go 1.22 method+pattern routing so an unknown /v1 path 404s and a wrong
// method 405s for free. The /v1 mux runs behind authenticate (identity
// resolution); Account-data routes additionally sit behind requireAuth. Every
// non-/v1 path is served by the embedded SPA (ADR 0005) when one is configured.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public: liveness and the static Catalog (reference data, not Account data).
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/metrics", s.handleMetrics)

	// Auth: login and logout are public entry points; me is protected.
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.Handle("GET /v1/auth/me", s.requireAuth(s.handleMe))

	// Account data: only the authenticated Account's own series.
	mux.Handle("GET /v1/series", s.requireAuth(s.handleSeries))

	// Dashboards and their Panels: Account-scoped CRUD backing the SPA.
	mux.Handle("GET /v1/dashboards", s.requireAuth(s.handleListDashboards))
	mux.Handle("POST /v1/dashboards", s.requireAuth(s.handleCreateDashboard))
	mux.Handle("GET /v1/dashboards/{id}", s.requireAuth(s.handleGetDashboard))
	mux.Handle("PATCH /v1/dashboards/{id}", s.requireAuth(s.handleUpdateDashboard))
	mux.Handle("DELETE /v1/dashboards/{id}", s.requireAuth(s.handleDeleteDashboard))
	mux.Handle("POST /v1/dashboards/{id}/panels", s.requireAuth(s.handleCreatePanel))
	mux.Handle("PATCH /v1/dashboards/{id}/panels/order", s.requireAuth(s.handleReorderPanels))
	mux.Handle("PATCH /v1/panels/{id}", s.requireAuth(s.handleUpdatePanel))
	mux.Handle("DELETE /v1/panels/{id}", s.requireAuth(s.handleDeletePanel))

	// Split the surface: /v1/* is the JSON API (identity-resolved), everything
	// else is the SPA. The mux's method+pattern routing 404s an unknown /v1 path
	// and 405s a known one with the wrong method for free; those routing-level
	// errors keep the stdlib's plain-text body, while every application error is
	// JSON.
	root := http.NewServeMux()
	root.Handle("/v1/", s.authenticate(mux))
	if s.spa != nil {
		root.Handle("/", s.spa)
	}
	return s.recoverPanic(root)
}
