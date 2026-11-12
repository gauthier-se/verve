package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	return openTestStore(t, filepath.Join(t.TempDir(), "verve.db"), clk)
}

// openTestStore returns a store on the migrated database at path, and the database. The store is
// closed at the end of the test.
func openTestStore(t *testing.T, path string, clk clock.Clock) (store.Store, *DB) {
	t.Helper()

	db, err := Open(t.Context(), path)
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

// Given the contract suite storetest.RunAuditEvents, then the SQLite implementation passes it.
func TestStore_AuditEventsContract(t *testing.T) {
	t.Parallel()

	storetest.RunAuditEvents(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunUsers, then the SQLite implementation passes it.
func TestStore_UsersContract(t *testing.T) {
	t.Parallel()

	storetest.RunUsers(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunSessions, then the SQLite implementation passes it.
func TestStore_SessionsContract(t *testing.T) {
	t.Parallel()

	storetest.RunSessions(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunPreferences, then the SQLite implementation passes it.
func TestStore_PreferencesContract(t *testing.T) {
	t.Parallel()

	storetest.RunPreferences(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunInvitations, then the SQLite implementation passes it.
func TestStore_InvitationsContract(t *testing.T) {
	t.Parallel()

	storetest.RunInvitations(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunTokens, then the SQLite implementation passes it.
func TestStore_TokensContract(t *testing.T) {
	t.Parallel()

	storetest.RunTokens(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunPasswordResetLinks, then the SQLite implementation passes
// it.
func TestStore_PasswordResetLinksContract(t *testing.T) {
	t.Parallel()

	storetest.RunPasswordResetLinks(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given two stores on one file, when both write concurrently in short transactions, then every
// write lands and no SQLITE_BUSY error surfaces.
func TestStore_ConcurrentWritesOnTwoStoresAllLand(t *testing.T) {
	t.Parallel()

	const (
		writersPerStore = 4
		writesPerWriter = 25
	)

	ctx := t.Context()
	clk := clock.NewFake(txTestNow)
	path := filepath.Join(t.TempDir(), "verve.db")

	first, _ := openTestStore(t, path, clk)
	second, _ := openTestStore(t, path, clk)
	stores := []store.Store{first, second}

	var wg sync.WaitGroup

	errs := make(chan error, len(stores)*writersPerStore*writesPerWriter)

	for i, s := range stores {
		for writer := range writersPerStore {
			wg.Go(func() {
				for write := range writesPerWriter {
					key := fmt.Sprintf("store-%d-writer-%d-write-%d", i, writer, write)
					if err := s.InstanceSettings().Set(ctx, key, "value"); err != nil {
						errs <- err
					}
				}
			})
		}
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	for _, s := range stores {
		settings, err := s.InstanceSettings().List(ctx)
		require.NoError(t, err)
		require.Len(t, settings, len(stores)*writersPerStore*writesPerWriter)
	}
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

	first, _ := openTestStore(t, path, clk)
	second, _ := openTestStore(t, path, clk)
	stores := []store.Store{first, second}

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
					job, _, err := s.Jobs().Claim(ctx, time.Minute)
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

// Given the contract suite storetest.RunSources, then the SQLite implementation passes it.
func TestStore_SourcesContract(t *testing.T) {
	t.Parallel()

	storetest.RunSources(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunMetricTypes, then the SQLite implementation passes it.
func TestStore_MetricTypesContract(t *testing.T) {
	t.Parallel()

	storetest.RunMetricTypes(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunSamples, then the SQLite implementation passes it.
func TestStore_SamplesContract(t *testing.T) {
	t.Parallel()

	storetest.RunSamples(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}

// Given the contract suite storetest.RunWorkouts, then the SQLite implementation passes it.
func TestStore_WorkoutsContract(t *testing.T) {
	t.Parallel()

	storetest.RunWorkouts(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		s, _ := newTestStore(t, clk)

		return s
	})
}
