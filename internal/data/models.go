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
	Phases       PhaseModel
	Pins         PinModel
	Annotations  AnnotationModel
	Exclusions   ExclusionModel
	Imports      ImportModel

	db *sql.DB // for cross-cutting needs (health checks, beginning a transaction)
	// tx is non-nil on a Models handed to Tx's callback: every model above is bound
	// to it, and Tx uses it to know it is already inside one.
	tx *sql.Tx
}

// NewModels wires the models to the connection pool.
func NewModels(db *sql.DB) Models {
	m := newModels(db)
	m.db = db
	return m
}

// newModels binds every model to one Handle, which is the pool for NewModels and
// the transaction for Tx.
func newModels(h Handle) Models {
	return Models{
		Accounts:     AccountModel{DB: h},
		AuthSessions: AuthSessionModel{DB: h},
		Measurements: MeasurementModel{DB: h},
		States:       StateModel{DB: h},
		Sessions:     SessionModel{DB: h},
		Dashboards:   DashboardModel{DB: h},
		Panels:       PanelModel{DB: h},
		Phases:       PhaseModel{DB: h},
		Pins:         PinModel{DB: h},
		Annotations:  AnnotationModel{DB: h},
		Exclusions:   ExclusionModel{DB: h},
		Imports:      ImportModel{DB: h},
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
	ImportModel
}

// ImportStore returns the family models bundled for a Connector import.
func (m Models) ImportStore() ImportStore {
	return ImportStore{m.Measurements, m.States, m.Sessions, m.Imports}
}
