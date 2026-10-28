package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// openTestDB opens a database in a fresh temporary directory and closes it at the end of the test.
func openTestDB(t *testing.T) (*DB, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "verve.db")

	db, err := Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(context.Background()) })

	return db, path
}

// createCounter creates a table and commits one row, so that tests have committed state to read.
func createCounter(t *testing.T, db *DB) {
	t.Helper()

	_, err := db.Write().ExecContext(t.Context(), "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(), "INSERT INTO items (name) VALUES ('committed')")
	require.NoError(t, err)
}

func countItems(ctx context.Context, t *testing.T, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
},
) int {
	t.Helper()

	var n int
	require.NoError(t, q.QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&n))

	return n
}

// Given Open on a path that does not exist, then verve.db is created with permissions 0600.
func TestOpen_CreatesDatabaseFileOwnerOnly(t *testing.T) {
	t.Parallel()

	_, path := openTestDB(t)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Given an opened database, then every connection of both pools reports journal_mode=wal,
// foreign_keys=1, busy_timeout=5000 and synchronous=1 (NORMAL).
func TestOpen_EveryConnectionOfBothPoolsHasThePragmas(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	ctx := t.Context()

	pools := map[string]struct {
		db    *sql.DB
		conns int
	}{
		"writer": {db: db.Write(), conns: 1},
		"reader": {db: db.Read(), conns: 4},
	}

	for name, pool := range pools {
		// Hold the connections open at the same time so that each one is a distinct connection.
		conns := make([]*sql.Conn, 0, pool.conns)

		for range pool.conns {
			conn, err := pool.db.Conn(ctx)
			require.NoError(t, err, name)

			conns = append(conns, conn)
		}

		for i, conn := range conns {
			var (
				journalMode string
				foreignKeys int
				busyTimeout int
				synchronous int
			)

			require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode))
			require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
			require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
			require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous))

			require.Equal(t, "wal", journalMode, "%s connection %d", name, i)
			require.Equal(t, 1, foreignKeys, "%s connection %d", name, i)
			require.Equal(t, 5000, busyTimeout, "%s connection %d", name, i)
			require.Equal(t, 1, synchronous, "%s connection %d", name, i)
		}

		for _, conn := range conns {
			require.NoError(t, conn.Close())
		}
	}
}

// Given the writer pool, then it has at most one open connection.
func TestOpen_WriterPoolHasOneConnection(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)

	require.Equal(t, 1, db.Write().Stats().MaxOpenConnections)
}

// Given the writer pool, then transactions start with BEGIN IMMEDIATE: an empty transaction
// already holds the write lock, so another connection cannot start writing.
func TestOpen_WriterTransactionsBeginImmediate(t *testing.T) {
	t.Parallel()

	db, path := openTestDB(t)
	ctx := t.Context()

	tx, err := db.Write().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	other, err := sql.Open(driverName, "file:"+path+"?_pragma=busy_timeout(0)")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, other.Close()) })

	_, err = other.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.ErrorContains(t, err, "SQLITE_BUSY")
}

// Given 20 goroutines writing concurrently through the writer, then all writes succeed without
// SQLITE_BUSY.
func TestWriter_ConcurrentWritesAllSucceed(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	createCounter(t, db)
	ctx := t.Context()

	const (
		writers         = 20
		writesPerWriter = 10
	)

	var wg sync.WaitGroup

	errs := make(chan error, writers*writesPerWriter)

	for range writers {
		wg.Go(func() {
			for range writesPerWriter {
				tx, err := db.Write().BeginTx(ctx, nil)
				if err != nil {
					errs <- err

					return
				}

				if _, err := tx.ExecContext(ctx, "INSERT INTO items (name) VALUES ('concurrent')"); err != nil {
					_ = tx.Rollback()
					errs <- err

					return
				}

				if err := tx.Commit(); err != nil {
					errs <- err

					return
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, 1+writers*writesPerWriter, countItems(ctx, t, db.Read()))
}

// Given a write statement executed on the reader pool, then it fails.
func TestReader_RejectsWrites(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	createCounter(t, db)
	ctx := t.Context()

	_, err := db.Read().ExecContext(ctx, "INSERT INTO items (name) VALUES ('from reader')")
	require.ErrorContains(t, err, "readonly")

	require.Equal(t, 1, countItems(ctx, t, db.Read()))
}

// Given a write transaction kept open, then reads on the reader pool still succeed and see the
// last committed state.
func TestReader_ReadsDuringOpenWriteTransaction(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	createCounter(t, db)
	ctx := t.Context()

	tx, err := db.Write().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	_, err = tx.ExecContext(ctx, "INSERT INTO items (name) VALUES ('uncommitted')")
	require.NoError(t, err)

	require.Equal(t, 2, countItems(ctx, t, tx))
	require.Equal(t, 1, countItems(ctx, t, db.Read()))
}

// Given Close, then the WAL is checkpointed with TRUNCATE and verve.db-wal is empty or absent,
// even while another connection keeps the database open.
func TestClose_CheckpointsTheWAL(t *testing.T) {
	t.Parallel()

	db, path := openTestDB(t)
	createCounter(t, db)
	ctx := t.Context()

	// A connection outside Verve (for example the sqlite3 shell) stops SQLite from removing the
	// WAL when Verve closes its last connection, so only an explicit checkpoint empties it.
	other, err := sql.Open(driverName, "file:"+path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, other.Close()) })

	var mode string
	require.NoError(t, other.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	require.Equal(t, "wal", mode)

	info, err := os.Stat(path + "-wal")
	require.NoError(t, err)
	require.Positive(t, info.Size(), "the WAL must contain the writes before Close")

	require.NoError(t, db.Close(ctx))

	info, err = os.Stat(path + "-wal")
	if !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
		require.Zero(t, info.Size())
	}

	require.Equal(t, 1, countItems(ctx, t, other))
}
