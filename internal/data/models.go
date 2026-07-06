package data

import (
	"context"
	"database/sql"
)

// Models aggregates every DAO-style model, so the rest of the app depends on a
// single injected value rather than reaching for *sql.DB directly.
type Models struct {
	Accounts     AccountModel
	AuthSessions AuthSessionModel
	Measurements MeasurementModel
	States       StateModel
	Sessions     SessionModel
	Dashboards   DashboardModel
	Panels       PanelModel

	// db is retained for cross-cutting concerns (e.g. health checks) that need
	// the handle itself rather than a specific DAO.
	db *sql.DB
}

// NewModels wires the models to a database handle.
func NewModels(db *sql.DB) Models {
	return Models{
		Accounts:     AccountModel{DB: db},
		AuthSessions: AuthSessionModel{DB: db},
		Measurements: MeasurementModel{DB: db},
		States:       StateModel{DB: db},
		Sessions:     SessionModel{DB: db},
		Dashboards:   DashboardModel{DB: db},
		Panels:       PanelModel{DB: db},
		db:           db,
	}
}

// Ping verifies the database is reachable, backing liveness checks without
// callers reaching through a DAO for the underlying handle.
func (m Models) Ping(ctx context.Context) error {
	return m.db.PingContext(ctx)
}
