// Package sqlite is the SQLite implementation of the Verve storage. Nothing outside this package
// and internal/app imports the SQLite driver.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// driverName is the database/sql name of the modernc.org/sqlite driver.
const driverName = "sqlite"

// dbFilePerm is the permission of the database file. SQLite gives the WAL and shared memory
// files the same permission.
const dbFilePerm = 0o600

// connMaxIdleTime closes connections idle for longer, so that a quiet instance holds few file
// descriptors.
const connMaxIdleTime = 5 * time.Minute

// pragmas apply to every connection of both pools.
const pragmas = "_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=synchronous(NORMAL)"

// DB is a SQLite database in WAL mode with two pools: a single writer connection whose
// transactions start with BEGIN IMMEDIATE, so writers queue instead of failing with
// SQLITE_BUSY, and a read-only pool that long writes never block.
type DB struct {
	write *sql.DB
	read  *sql.DB
}

// Open opens the database at path, creating the file with permissions 0600 when it does not exist.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := createFile(path); err != nil {
		return nil, err
	}

	write, err := openPool(ctx, "file:"+path+"?"+pragmas+"&_txlock=immediate", 1)
	if err != nil {
		return nil, fmt.Errorf("open database writer: %w", err)
	}

	read, err := openPool(ctx, "file:"+path+"?"+pragmas+"&_pragma=query_only(ON)", max(4, runtime.NumCPU()))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open database reader: %w", err), write.Close())
	}

	return &DB{write: write, read: read}, nil
}

// createFile creates the database file with its permission before SQLite does, because SQLite
// would create it with a mode readable by everyone.
func createFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, dbFilePerm) //nolint:gosec // G304: the path comes from the configured data directory
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("create database file: %w", err)
	}

	return nil
}

func openPool(ctx context.Context, dsn string, maxConns int) (*sql.DB, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxIdleTime(connMaxIdleTime)

	// Connect once so that a broken file or DSN fails at startup, and the writer switches the
	// database to WAL before any reader connects.
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	return db, nil
}

// Write returns the writer pool. It holds a single connection: writers queue for it.
func (db *DB) Write() *sql.DB {
	return db.write
}

// Read returns the read-only pool. Write statements on it fail.
func (db *DB) Read() *sql.DB {
	return db.read
}

// Close checkpoints the WAL into the database file, truncates it and closes both pools.
func (db *DB) Close(ctx context.Context) error {
	readErr := db.read.Close()

	checkpointErr := db.checkpoint(ctx)

	if err := errors.Join(readErr, checkpointErr, db.write.Close()); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

// checkpoint copies the whole WAL into the database file and truncates it.
func (db *DB) checkpoint(ctx context.Context) error {
	var busy, logFrames, checkpointed int

	err := db.write.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	if err != nil {
		return fmt.Errorf("checkpoint database: %w", err)
	}

	if busy != 0 {
		return errors.New("checkpoint database: blocked by another connection")
	}

	return nil
}
