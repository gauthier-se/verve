// Package api is Verve's HTTP layer: a net/http server exposing the query engine
// as a JSON API of aggregated buckets (ADR 0012), scoped to the authenticated
// Account (ADR 0007) via an opaque session cookie (ADR 0008).
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
	// SPA serves the embedded front-end on every non-/v1 path (ADR 0005); injected
	// to decouple from web-assets. Nil means API-only.
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
	// A failed login always costs an argon2 verify against decoyHash, so timing
	// doesn't reveal whether the email exists.
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

// Handler builds the routed, panic-recovering http.Handler. Go 1.22 method+pattern
// routing gives 404/405 for free; /v1 runs behind authenticate, Account-data routes
// behind requireAuth, and non-/v1 paths hit the embedded SPA (ADR 0005).
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

	// /v1/* is the JSON API (identity-resolved), everything else the SPA. Routing-
	// level 404/405 keep the stdlib plain-text body; application errors are JSON.
	root := http.NewServeMux()
	root.Handle("/v1/", s.authenticate(mux))
	if s.spa != nil {
		root.Handle("/", s.spa)
	}
	return s.recoverPanic(root)
}
