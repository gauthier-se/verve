package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// openSnapshot opens the snapshot at path as a store and requires that it passes the integrity check.
func openSnapshot(t *testing.T, path string) store.Store {
	t.Helper()

	db, err := Open(t.Context(), path)
	require.NoError(t, err)

	var integrity string
	require.NoError(t, db.Read().QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)

	s := New(db, clock.Real())
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s
}

// Given a write transaction that holds the writer, when Snapshot runs, then it completes without
// waiting for the writer, and the copy holds the committed writes but not the uncommitted ones.
func TestSnapshot_WriteInProgress_DoesNotWaitForTheWriter(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	require.NoError(t, s.InstanceSettings().Set(ctx, "map.mode", "none"))

	path := filepath.Join(t.TempDir(), "snapshot.db")

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := tx.InstanceSettings().Set(ctx, "smtp.host", "mail.example.com"); err != nil {
			return err
		}

		return s.Snapshot(ctx, path)
	})
	require.NoError(t, err)

	snapshot := openSnapshot(t, path)
	requireSetting(ctx, t, snapshot, "map.mode", "none")
	requireNoSetting(ctx, t, snapshot, "smtp.host")
}

// Given writers that commit pairs of settings, when Snapshot runs meanwhile, then the copy passes
// the integrity check and holds whole pairs only.
func TestSnapshot_ConcurrentWrites_CopyIsConsistent(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.Real())
	ctx := boundedContext(t)

	writeCtx, stopWriting := context.WithCancel(ctx)

	var (
		wg       sync.WaitGroup
		writeErr error
	)

	wg.Go(func() {
		for i := 0; writeCtx.Err() == nil; i++ {
			err := s.WithTx(writeCtx, func(ctx context.Context, tx store.Repositories) error {
				if err := tx.InstanceSettings().Set(ctx, fmt.Sprintf("pair.%d.a", i), "a"); err != nil {
					return err
				}

				return tx.InstanceSettings().Set(ctx, fmt.Sprintf("pair.%d.b", i), "b")
			})
			if err != nil && writeCtx.Err() == nil {
				writeErr = err

				return
			}
		}
	})

	// Let the writers commit a few pairs first, so that the snapshot copies some.
	require.Eventually(t, func() bool {
		settings, err := s.InstanceSettings().List(ctx)

		return err == nil && len(settings) >= 20
	}, 3*time.Second, 5*time.Millisecond)

	path := filepath.Join(t.TempDir(), "snapshot.db")
	snapshotErr := s.Snapshot(ctx, path)

	stopWriting()
	wg.Wait()

	require.NoError(t, snapshotErr)
	require.NoError(t, writeErr)

	settings, err := openSnapshot(t, path).InstanceSettings().List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, settings)

	pairs := map[string]int{}
	for _, setting := range settings {
		pairs[strings.TrimSuffix(strings.TrimSuffix(setting.Key, ".a"), ".b")]++
	}

	for pair, n := range pairs {
		require.Equal(t, 2, n, "pair %s must be whole", pair)
	}
}

// Given a file at the path, then Snapshot fails and leaves the file as it was.
func TestSnapshot_ExistingFile_Fails(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	path := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, os.WriteFile(path, []byte("keep"), 0o600))

	require.Error(t, s.Snapshot(ctx, path))

	content, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)
	require.Equal(t, "keep", string(content))
}
