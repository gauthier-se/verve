// Package data is Verve's storage layer: opening the SQLite database, applying
// embedded migrations, and DAO-style models per canonical family. It holds no
// global state; a *sql.DB is opened once and injected where needed.
package data

import (
	"database/sql"
	"errors"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrRecordNotFound is returned by model getters when no row matches.
var ErrRecordNotFound = errors.New("data: record not found")

// pragmas are applied to every pooled SQLite connection via the DSN, so they
// hold regardless of which connection the pool hands out.
var pragmas = []string{
	"journal_mode(WAL)",  // concurrent readers alongside a writer
	"foreign_keys(1)",    // enforce referential integrity (off by default in SQLite)
	"busy_timeout(5000)", // wait up to 5s on a locked db instead of failing
}

// DSN builds a modernc.org/sqlite data-source name for the database file at
// path, with Verve's standard pragmas attached as connection parameters.
func DSN(path string) string {
	parts := make([]string, 0, len(pragmas))
	for _, p := range pragmas {
		parts = append(parts, "_pragma="+p)
	}
	return "file:" + path + "?" + strings.Join(parts, "&")
}

// Open opens (creating if absent) the SQLite database at path and verifies the
// connection. The caller owns the returned *sql.DB and must Close it.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	// SQLite tolerates a single writer only; a bounded pool keeps behaviour
	// predictable while WAL still allows concurrent reads.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
