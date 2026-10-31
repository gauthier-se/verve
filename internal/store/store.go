// Package store declares the storage interfaces that domain code depends on. Implementations live
// in sub-packages (internal/store/sqlite) and pass the contract suites of internal/store/storetest.
package store

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when the requested record does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict is returned when a write conflicts with an existing record.
	ErrConflict = errors.New("store: conflict")
)

// Repositories gives access to every repository. Used outside WithTx, each write is committed on
// its own when it returns.
type Repositories interface {
	InstanceSettings() InstanceSettingsRepository
	Jobs() JobsRepository

	// AfterCommit calls fn once the writes made through these repositories are committed: at once
	// outside WithTx, where each write is committed when it returns, and after the commit inside
	// WithTx, in the order of the calls. fn does not run when the transaction rolls back.
	AfterCommit(fn func())
}

// Store is the storage of Verve.
type Store interface {
	Repositories

	// WithTx runs fn in a single transaction, through the repositories passed as tx. The writes of
	// fn are committed when it returns nil. When fn returns an error, they are rolled back and that
	// error is returned. When fn panics, they are rolled back and the panic propagates.
	//
	// fn must use tx only: implementations may have a single writer, which fn holds, so a write
	// through the Store inside fn can wait forever.
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Repositories) error) error

	// Close releases the storage.
	Close(ctx context.Context) error
}

// Setting is an admin-managed instance setting.
type Setting struct {
	Key   string
	Value string
	// UpdatedAt is the time of the last Set, in UTC with millisecond precision.
	UpdatedAt time.Time
}

// InstanceSettingsRepository stores the admin-managed instance settings.
type InstanceSettingsRepository interface {
	// Get returns the setting of key, or ErrNotFound.
	Get(ctx context.Context, key string) (Setting, error)
	// List returns every setting, sorted by key.
	List(ctx context.Context) ([]Setting, error)
	// Set creates or replaces the setting of key, with the current time as UpdatedAt.
	Set(ctx context.Context, key, value string) error
	// Delete removes the setting of key, or returns ErrNotFound.
	Delete(ctx context.Context, key string) error
}
