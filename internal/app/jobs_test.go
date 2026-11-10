package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// jobsTest holds a store on a migrated database and a worker with the job kinds of the server.
type jobsTest struct {
	store  store.Store
	worker *jobs.Worker
}

func newJobsTest(t *testing.T) jobsTest {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), dbFile))
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	clk := clock.NewFake(time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	st := sqlite.New(db, clk)

	t.Cleanup(func() { require.NoError(t, st.Close(context.Background())) })

	registry := jobs.NewRegistry()
	registerJobs(registry, st, clk, config.Config{})

	return jobsTest{store: st, worker: jobs.NewWorker(st, registry, clk, slog.New(slog.DiscardHandler), jobs.Options{})}
}

// mustCreateUser stores an active member named username with status.
func (jt jobsTest) mustCreateUser(t *testing.T, username string, status store.UserStatus) store.User {
	t.Helper()

	user, err := jt.store.Users().Create(t.Context(), store.NewUser{Username: username, Role: store.RoleMember})
	require.NoError(t, err)

	if status != store.UserActive {
		user, err = jt.store.Users().SetStatus(t.Context(), user.ID, status)
		require.NoError(t, err)
	}

	return user
}

// runDeletion enqueues the account.delete job of user, runs it once with the worker and returns
// the job as it ended.
func (jt jobsTest) runDeletion(t *testing.T, user store.User) store.Job {
	t.Helper()

	queued, err := jt.store.Jobs().Enqueue(t.Context(), store.NewJob{
		Kind:    auth.KindDeleteAccount,
		Payload: json.RawMessage(fmt.Sprintf(`{"userId":%d}`, user.ID)),
	})
	require.NoError(t, err)

	claimed, err := jt.worker.Tick(t.Context())
	require.NoError(t, err)
	require.True(t, claimed)

	job, err := jt.store.Jobs().Get(t.Context(), queued.ID)
	require.NoError(t, err)

	return job
}

// Given the account.delete job of a user being deleted, then the worker runs it to completion and
// the user is gone.
func TestJobs_AccountDeleteRemovesTheUser(t *testing.T) {
	t.Parallel()

	jt := newJobsTest(t)
	bob := jt.mustCreateUser(t, "bob", store.UserDeleting)

	require.Equal(t, store.JobCompleted, jt.runDeletion(t, bob).Status)

	_, err := jt.store.Users().Get(t.Context(), bob.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Given the account.delete job of a user who is not being deleted, then it fails at once, without
// a retry, and the user stays.
func TestJobs_AccountDeleteOfAUserNotBeingDeletedFailsAtOnce(t *testing.T) {
	t.Parallel()

	jt := newJobsTest(t)
	bob := jt.mustCreateUser(t, "bob", store.UserActive)

	job := jt.runDeletion(t, bob)
	require.Equal(t, store.JobFailed, job.Status)
	require.Equal(t, int64(1), job.Attempts)

	_, err := jt.store.Users().Get(t.Context(), bob.ID)
	require.NoError(t, err)
}

// Given a running job of the user being deleted, then the account.delete job goes back to queued
// for a retry, and the user stays until it runs again.
func TestJobs_AccountDeleteWaitsForTheRunningJobsOfTheUser(t *testing.T) {
	t.Parallel()

	jt := newJobsTest(t)
	bob := jt.mustCreateUser(t, "bob", store.UserDeleting)

	_, err := jt.store.Jobs().Enqueue(t.Context(), store.NewJob{Kind: "test.import", Payload: json.RawMessage(`{}`), UserID: bob.ID})
	require.NoError(t, err)

	_, _, err = jt.store.Jobs().Claim(t.Context(), time.Minute)
	require.NoError(t, err)

	job := jt.runDeletion(t, bob)
	require.Equal(t, store.JobQueued, job.Status)
	require.Equal(t, int64(1), job.Attempts)

	_, err = jt.store.Users().Get(t.Context(), bob.ID)
	require.NoError(t, err)
}
