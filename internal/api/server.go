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
	"github.com/gauthier-se/verve/internal/estimate"
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
	// DataDir is the data root; web import uploads stream through DataDir/tmp and
	// orphans there are swept at startup (ADR 0016).
	DataDir string
	// ArtifactsDir is where a web import copies GPX route artifacts (ADR 0004), and
	// where the workout endpoints read them back from.
	ArtifactsDir string
	// MaxUploadBytes caps a web import upload; zero uses defaultMaxUploadBytes.
	MaxUploadBytes int64
	// MapTiles is the tile URL template a workout map draws its basemap from, and
	// is empty by default (ADR 0028). Empty means the trace is drawn on a blank
	// ground and the browser makes no outbound request, which is what keeps "does
	// not phone home" literally true. Filling it is the account holder informed
	// choice, and it is echoed to the client so the map can render its attribution.
	MapTiles string
	// MapAttribution is the credit line the configured tile source requires. It is
	// not optional in practice: every public tile server asks for one.
	MapAttribution string
}

// Server holds the HTTP layer's dependencies. It owns no global state.
type Server struct {
	logger        *slog.Logger
	models        data.Models
	engine        query.Engine
	estimates     estimate.Engine
	resolver      authResolver
	loginLimiter  *loginLimiter
	secureCookies bool
	sessionTTL    time.Duration
	spa           http.Handler
	artifactsDir  string
	mapTiles      string
	mapAttrib     string
	imports       *importRegistry
	// decoyHash is verified against on logins for missing accounts so timing does
	// not reveal which emails exist. It is a hash of an unguessable value.
	decoyHash string
}

// New builds a Server. cfg tunes cookie security, session lifetime, and the web
// import (data dir, artifacts dir, upload cap). Building the import registry
// prepares its temp dir and sweeps orphan uploads, so it can fail (ADR 0016).
func New(logger *slog.Logger, models data.Models, engine query.Engine, cfg Config) (*Server, error) {
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

	imports, err := newImportRegistry(logger, models.ImportStore(), cfg.DataDir, cfg.ArtifactsDir, cfg.MaxUploadBytes)
	if err != nil {
		return nil, err
	}

	return &Server{
		logger:        logger,
		models:        models,
		engine:        engine,
		estimates:     estimate.Engine{Query: engine},
		resolver:      sessionResolver{sessions: models.AuthSessions},
		loginLimiter:  newLoginLimiter(),
		secureCookies: cfg.SecureCookies,
		sessionTTL:    ttl,
		spa:           cfg.SPA,
		artifactsDir:  cfg.ArtifactsDir,
		mapTiles:      cfg.MapTiles,
		mapAttrib:     cfg.MapAttribution,
		imports:       imports,
		decoyHash:     decoy,
	}, nil
}

// Handler builds the routed, panic-recovering http.Handler. Go 1.22 method+pattern
// routing gives 404/405 for free; /v1 runs behind authenticate, Account-data routes
// behind requireAuth, and non-/v1 paths hit the embedded SPA (ADR 0005).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public: liveness and the static Catalog (reference data, not Account data).
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/metrics", s.handleMetrics)

	// Auth: state, login, and first-run register are public entry points; me is
	// protected. register is open only while the instance has zero Accounts (ADR 0017).
	mux.HandleFunc("GET /v1/auth/state", s.handleAuthState)
	mux.HandleFunc("POST /v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.Handle("GET /v1/auth/me", s.requireAuth(s.handleMe))

	// Account data: only the authenticated Account's own series.
	mux.Handle("GET /v1/series", s.requireAuth(s.handleSeries))

	// The resolved time axis behind a set of range tokens: the window, its bucket,
	// and the compared window. Dates the interface prints come from here, never from
	// the browser's clock (ADR 0012).
	mux.Handle("GET /v1/timeaxis", s.requireAuth(s.handleTimeAxis))

	// The Ledger overview: one folded row per Metric with data (ADR 0021).
	mux.Handle("GET /v1/ledger", s.requireAuth(s.handleLedger))

	// Manual entries: the Account's own write path into the measurement store
	// (ADR 0022). DELETE refuses anything but a Manual row.
	mux.Handle("POST /v1/measurements", s.requireAuth(s.handleCreateMeasurement))
	mux.Handle("GET /v1/measurements", s.requireAuth(s.handleListMeasurements))
	mux.Handle("DELETE /v1/measurements/{id}", s.requireAuth(s.handleDeleteMeasurement))

	// The Plan: Estimates, Targets and Phases (ADR 0023). GET /v1/plan answers the whole
	// page in one call, deriving the targets server-side so the client never re-computes.
	mux.Handle("GET /v1/plan", s.requireAuth(s.handlePlan))
	mux.Handle("GET /v1/phases", s.requireAuth(s.handleListPhases))
	mux.Handle("POST /v1/phases", s.requireAuth(s.handleOpenPhase))
	mux.Handle("PATCH /v1/phases/{id}", s.requireAuth(s.handleClosePhase))
	mux.Handle("DELETE /v1/phases/{id}", s.requireAuth(s.handleDeletePhase))

	// Profile: the Account attributes that are not Measurements (date of birth,
	// biological sex, body-composition trust).
	mux.Handle("GET /v1/profile", s.requireAuth(s.handleGetProfile))
	mux.Handle("PATCH /v1/profile", s.requireAuth(s.handleUpdateProfile))

	// Web self-service import: upload a .zip, then poll the job (ADR 0016).
	mux.Handle("POST /v1/imports", s.requireAuth(s.handleCreateImport))
	mux.Handle("GET /v1/imports", s.requireAuth(s.handleImportStatus))

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

	// Workouts (ADR 0028): a Session is an entity, so it is listed and opened
	// rather than bucketed, and its Route is served as its own resource.
	mux.Handle("GET /v1/sessions", s.requireAuth(s.handleListSessions))
	mux.Handle("GET /v1/sessions/{id}", s.requireAuth(s.handleGetSession))
	mux.Handle("GET /v1/sessions/{id}/routes", s.requireAuth(s.handleSessionRoutes))
	mux.Handle("GET /v1/sessions/{id}/routes/{routeID}", s.requireAuth(s.handleDownloadRoute))

	// Pins: the Metrics the Account keeps in the sidebar. A Pin is a shortcut to a
	// Metric page, so its identity is the Catalog slug and both writes are idempotent.
	mux.Handle("GET /v1/pins", s.requireAuth(s.handleListPins))
	mux.Handle("POST /v1/pins", s.requireAuth(s.handleCreatePin))
	mux.Handle("DELETE /v1/pins/{metric}", s.requireAuth(s.handleDeletePin))

	// History: the long view — everything the Account holds, and the dated events that
	// explain its shape. One call, because the band, its Phases and the ledger all have
	// to agree about one axis.
	mux.Handle("GET /v1/history", s.requireAuth(s.handleHistory))

	// Co-variation: the pinned Metrics paired over one window at one lag. The read is
	// quadratic, so its inputs are the Pins and its width is capped (ADR 0025).
	mux.Handle("GET /v1/covary", s.requireAuth(s.handleCoVary))

	// Annotations: dated notes on the time axis (ADR 0030). The list takes the same
	// time-axis tokens /v1/series takes, and answers with each note folded onto that
	// bucket grid; with no tokens it answers the Account's whole history.
	mux.Handle("GET /v1/annotations", s.requireAuth(s.handleListAnnotations))
	mux.Handle("POST /v1/annotations", s.requireAuth(s.handleCreateAnnotation))
	mux.Handle("PATCH /v1/annotations/{id}", s.requireAuth(s.handleUpdateAnnotation))
	mux.Handle("DELETE /v1/annotations/{id}", s.requireAuth(s.handleDeleteAnnotation))

	// /v1/* is the JSON API (identity-resolved), everything else the SPA. Routing-
	// level 404/405 keep the stdlib plain-text body; application errors are JSON.
	root := http.NewServeMux()
	root.Handle("/v1/", s.authenticate(mux))
	if s.spa != nil {
		root.Handle("/", s.spa)
	}
	return s.recoverPanic(root)
}
