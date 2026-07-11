package data

import (
	"context"
	"database/sql"
)

// Models aggregates every DAO model so the app depends on one injected value.
type Models struct {
	Accounts     AccountModel
	AuthSessions AuthSessionModel
	Measurements MeasurementModel
	States       StateModel
	Sessions     SessionModel
	Dashboards   DashboardModel
	Panels       PanelModel

	db *sql.DB // for cross-cutting needs (e.g. health checks) that want the handle itself
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

// Ping verifies the database is reachable, backing liveness checks.
func (m Models) Ping(ctx context.Context) error {
	return m.db.PingContext(ctx)
}

// ImportStore bundles the family models a Connector import writes through; its
// embedded methods together satisfy applehealth.Store (kept here, not in that
// package, to avoid an import cycle). Both the CLI and web import use it, so a new
// family is a one-place edit.
type ImportStore struct {
	MeasurementModel
	StateModel
	SessionModel
}

// ImportStore returns the family models bundled for a Connector import.
func (m Models) ImportStore() ImportStore {
	return ImportStore{m.Measurements, m.States, m.Sessions}
}
