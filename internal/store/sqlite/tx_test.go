package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

var txTestNow = time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)

// boundedContext returns a context that expires well before the test timeout, so that a read or
// write waiting for a connection held by the test fails instead of hanging.
func boundedContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func requireSetting(ctx context.Context, t *testing.T, repos store.Repositories, key, want string) {
	t.Helper()

	got, err := repos.InstanceSettings().Get(ctx, key)
	require.NoError(t, err, key)
	require.Equal(t, want, got.Value, key)
}

func requireNoSetting(ctx context.Context, t *testing.T, repos store.Repositories, key string) {
	t.Helper()

	_, err := repos.InstanceSettings().Get(ctx, key)
	require.ErrorIs(t, err, store.ErrNotFound, key)
}

// Given WithTx whose function returns nil, then its writes are committed and visible outside.
func TestWithTx_CommitsWhenTheFunctionSucceeds(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := tx.InstanceSettings().Set(ctx, "map.mode", "none"); err != nil {
			return err
		}

		return tx.InstanceSettings().Set(ctx, "smtp.host", "mail.example.com")
	})
	require.NoError(t, err)

	requireSetting(ctx, t, s, "map.mode", "none")
	requireSetting(ctx, t, s, "smtp.host", "mail.example.com")
}

// Given WithTx whose function returns an error, then every write inside is rolled back and the
// same error is returned.
func TestWithTx_FunctionErrorRollsBackEveryWriteAndReturnsTheError(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	require.NoError(t, s.InstanceSettings().Set(ctx, "map.mode", "none"))
	require.NoError(t, s.InstanceSettings().Set(ctx, "auth.oidc.enabled", "true"))

	errInvalid := errors.New("invalid settings")

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		settings := tx.InstanceSettings()

		if err := settings.Set(ctx, "map.mode", "pmtiles"); err != nil {
			return err
		}

		if err := settings.Set(ctx, "smtp.host", "mail.example.com"); err != nil {
			return err
		}

		if err := settings.Delete(ctx, "auth.oidc.enabled"); err != nil {
			return err
		}

		return fmt.Errorf("validate: %w", errInvalid)
	})
	require.ErrorIs(t, err, errInvalid)

	requireSetting(ctx, t, s, "map.mode", "none")
	requireSetting(ctx, t, s, "auth.oidc.enabled", "true")
	requireNoSetting(ctx, t, s, "smtp.host")
}

// Given WithTx whose function panics, then the transaction is rolled back, the panic is
// re-raised and the writer is free for the next write.
func TestWithTx_PanicRollsBackAndIsReRaised(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	require.PanicsWithValue(t, "boom", func() {
		_ = s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			if err := tx.InstanceSettings().Set(ctx, "map.mode", "none"); err != nil {
				return err
			}

			panic("boom")
		})
	})

	requireNoSetting(ctx, t, s, "map.mode")

	require.NoError(t, s.InstanceSettings().Set(ctx, "smtp.host", "mail.example.com"))
	requireSetting(ctx, t, s, "smtp.host", "mail.example.com")
}

// Given AfterCommit outside WithTx, where each write is committed when it returns, then fn runs at
// once.
func TestAfterCommit_OutsideWithTx_RunsAtOnce(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ran := false

	s.AfterCommit(func() { ran = true })

	require.True(t, ran)
}

// Given AfterCommit inside WithTx, then fn runs after the commit, in the order of the calls, and
// sees the writes of the transaction from outside.
func TestAfterCommit_InsideWithTx_RunsAfterTheCommit(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	var calls []string

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		tx.AfterCommit(func() {
			calls = append(calls, "first")

			requireSetting(ctx, t, s, "map.mode", "none")
		})
		tx.AfterCommit(func() { calls = append(calls, "second") })

		require.Empty(t, calls, "nothing runs before the commit")

		return tx.InstanceSettings().Set(ctx, "map.mode", "none")
	})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, calls)
}

// Given AfterCommit inside a WithTx that rolls back, then fn does not run.
func TestAfterCommit_InsideWithTxThatRollsBack_DoesNotRun(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)
	errRollback := errors.New("roll back")
	ran := false

	err := s.WithTx(ctx, func(_ context.Context, tx store.Repositories) error {
		tx.AfterCommit(func() { ran = true })

		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	require.False(t, ran)

	require.PanicsWithValue(t, "boom", func() {
		_ = s.WithTx(ctx, func(_ context.Context, tx store.Repositories) error {
			tx.AfterCommit(func() { ran = true })

			panic("boom")
		})
	})
	require.False(t, ran)
}

// Given WithTx, then reads through the transaction see its uncommitted writes.
func TestWithTx_ReadsInsideSeeTheTransactionWrites(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	require.NoError(t, s.InstanceSettings().Set(ctx, "map.mode", "none"))

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		settings := tx.InstanceSettings()

		if err := settings.Set(ctx, "map.mode", "pmtiles"); err != nil {
			return err
		}

		if err := settings.Set(ctx, "smtp.host", "mail.example.com"); err != nil {
			return err
		}

		requireSetting(ctx, t, tx, "map.mode", "pmtiles")

		all, err := settings.List(ctx)
		require.NoError(t, err)
		require.Len(t, all, 2)

		return nil
	})
	require.NoError(t, err)
}

// Given repositories used outside WithTx, then reads use the reader pool: while a transaction
// holds the single writer connection with uncommitted writes, they return the last committed
// state without waiting for the writer.
func TestStore_ReadsOutsideWithTxUseTheReaderPool(t *testing.T) {
	t.Parallel()

	s, _ := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	require.NoError(t, s.InstanceSettings().Set(ctx, "map.mode", "none"))

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := tx.InstanceSettings().Set(ctx, "map.mode", "pmtiles"); err != nil {
			return err
		}

		if err := tx.InstanceSettings().Set(ctx, "smtp.host", "mail.example.com"); err != nil {
			return err
		}

		requireSetting(ctx, t, s, "map.mode", "none")
		requireNoSetting(ctx, t, s, "smtp.host")

		all, err := s.InstanceSettings().List(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)

		return nil
	})
	require.NoError(t, err)
}

// Given repositories used outside WithTx, then each write runs in its own transaction on the
// writer: writes from 20 goroutines queue behind a transaction in progress instead of failing
// with SQLITE_BUSY, and each one is committed when it returns.
func TestStore_WritesOutsideWithTxEachCommitOnTheWriter(t *testing.T) {
	t.Parallel()

	s, db := newTestStore(t, clock.NewFake(txTestNow))
	ctx := boundedContext(t)

	const writers = 20

	holding := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)

	go func() {
		txDone <- s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			if err := tx.InstanceSettings().Set(ctx, "map.mode", "none"); err != nil {
				return err
			}

			close(holding)
			<-release

			return nil
		})
	}()

	select {
	case <-holding:
	case err := <-txDone:
		t.Fatalf("WithTx returned before running its function: %v", err)
	}

	var wg sync.WaitGroup

	errs := make(chan error, writers)

	for i := range writers {
		wg.Go(func() {
			key := fmt.Sprintf("writer.%02d", i)
			if err := s.InstanceSettings().Set(ctx, key, "set"); err != nil {
				errs <- err

				return
			}

			// Committed as soon as Set returns: visible through the reader pool.
			if _, err := s.InstanceSettings().Get(ctx, key); err != nil {
				errs <- err
			}
		})
	}

	// Every write waits for the writer connection that the transaction holds.
	require.Eventually(t, func() bool {
		return db.Write().Stats().WaitCount >= writers
	}, 5*time.Second, 5*time.Millisecond, "the writes must queue for the writer")

	close(release)
	require.NoError(t, <-txDone)

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	all, err := s.InstanceSettings().List(ctx)
	require.NoError(t, err)
	require.Len(t, all, writers+1)
}
