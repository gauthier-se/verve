package data

import "database/sql"

// Models aggregates every DAO-style model, so the rest of the app depends on a
// single injected value rather than reaching for *sql.DB directly.
type Models struct {
	Accounts     AccountModel
	Measurements MeasurementModel
	States       StateModel
	Sessions     SessionModel
}

// NewModels wires the models to a database handle.
func NewModels(db *sql.DB) Models {
	return Models{
		Accounts:     AccountModel{DB: db},
		Measurements: MeasurementModel{DB: db},
		States:       StateModel{DB: db},
		Sessions:     SessionModel{DB: db},
	}
}
