package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/storetest"
)

// newTestStore returns a store on a migrated database in a fresh temporary directory, and the
// database for checks on its pools. The store is closed at the end of the test.
func newTestStore(t *testing.T, clk clock.Clock) (store.Store, *DB) {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = Migrate(t.Context(), db, MigrateOptions{})
	require.NoError(t, err)

	s := New(db, clk)
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s, db
}

// Given the contract suite storetest.RunInstanceSettings, then the SQLite implementation passes it.
func TestStore_InstanceSettingsContract(t *testing.T) {
	t.Parallel()

	storetest.RunInstanceSettings(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunJobs, then the SQLite implementation passes it.
func TestStore_JobsContract(t *testing.T) {
	t.Parallel()

	storetest.RunJobs(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given two stores on one file claiming N jobs concurrently, then each job is claimed exactly once.
func TestJobs_ConcurrentClaimsOnTwoStoresClaimEachJobOnce(t *testing.T) {
	t.Parallel()

	const (
		jobCount         = 60
		claimersPerStore = 2
	)

	ctx := t.Context()
	clk := clock.NewFake(txTestNow)
	path := filepath.Join(t.TempDir(), "verve.db")

	openStore := func() store.Store {
		db, err := Open(ctx, path)
		require.NoError(t, err)

		_, err = Migrate(ctx, db, MigrateOptions{})
		require.NoError(t, err)

		s := New(db, clk)
		t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

		return s
	}

	stores := []store.Store{openStore(), openStore()}

	for range jobCount {
		_, err := stores[0].Jobs().Enqueue(ctx, store.NewJob{
			Kind:    "test.job",
			Payload: json.RawMessage(`{}`),
			RunAt:   txTestNow,
		})
		require.NoError(t, err)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = make(map[int64]int, jobCount)
	)

	errs := make(chan error, len(stores)*claimersPerStore)

	for _, s := range stores {
		for range claimersPerStore {
			wg.Go(func() {
				for {
					job, err := s.Jobs().Claim(ctx, time.Minute)
					if errors.Is(err, store.ErrNotFound) {
						return
					}

					if err != nil {
						errs <- err

						return
					}

					mu.Lock()
					claimed[job.ID]++
					mu.Unlock()
				}
			})
		}
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.Len(t, claimed, jobCount)

	for id, claims := range claimed {
		require.Equal(t, 1, claims, "job %d", id)
	}
}
