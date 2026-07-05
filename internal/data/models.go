package data

import "database/sql"

// Models aggregates every DAO-style model, so the rest of the app depends on a
// single injected value rather than reaching for *sql.DB directly.
type Models struct {
	Accounts AccountModel
}

// NewModels wires the models to a database handle.
func NewModels(db *sql.DB) Models {
	return Models{
		Accounts: AccountModel{DB: db},
	}
}
