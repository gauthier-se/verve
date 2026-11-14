package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/ingest/native"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// jobsTest holds a store on a migrated database, an inbox in a temporary directory, and a worker with
// the job kinds and the recurring jobs of the server, all on one fake clock.
type jobsTest struct {
	store     store.Store
	clock     *clock.Fake
	inboxDir  string
	worker    *jobs.Worker
	schedules []jobs.Schedule
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

	shipped, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, st.MetricTypes().Sync(t.Context(), shipped))

	inboxDir := t.TempDir()

	ing, err := newIngestion(st, shipped, inboxDir, clk)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ing.Close()) })

	registry := jobs.NewRegistry()
	schedules := registerJobs(registry, st, clk, config.Config{}, ing)

	return jobsTest{
		store:     st,
		clock:     clk,
		inboxDir:  inboxDir,
		worker:    jobs.NewWorker(st, registry, clk, slog.New(slog.DiscardHandler), jobs.Options{}),
		schedules: schedules,
	}
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

// Given the recurring jobs of the server, then cleanup.inbox runs every day, and its job removes
// the files of the inbox that no import needs once they are older than one hour.
func TestJobs_CleanupInboxRunsDailyAndRemovesOldFilesWithoutImport(t *testing.T) {
	t.Parallel()

	jt := newJobsTest(t)

	var schedule jobs.Schedule

	for _, s := range jt.schedules {
		if s.Name == "cleanup.inbox" {
			schedule = s
		}
	}

	require.Equal(t, "cleanup.inbox", schedule.Kind)
	require.Equal(t, 24*time.Hour, schedule.Every)

	old := filepath.Join(jt.inboxDir, "without-import")
	require.NoError(t, os.WriteFile(old, []byte(`{}`), 0o600))

	modified := jt.clock.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(old, modified, modified))

	_, err := jt.store.Jobs().Enqueue(t.Context(), store.NewJob{Kind: schedule.Kind, Payload: schedule.Payload})
	require.NoError(t, err)

	claimed, err := jt.worker.Tick(t.Context())
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoFileExists(t, old)
}

// Given a native push in the inbox, then the import.native job of the server processes it to
// completion.
func TestJobs_ImportNativeCompletesAPush(t *testing.T) {
	t.Parallel()

	jt := newJobsTest(t)
	ctx := t.Context()
	alice := jt.mustCreateUser(t, "alice", store.UserActive)

	name := "0f6c2d8e4b1a9f3c7e5d2b8a6c4e1f09"
	require.NoError(t, os.WriteFile(filepath.Join(jt.inboxDir, name),
		[]byte(`{"format":"verve.ingest.v1","client":{"name":"Verve for iOS","version":"1.0.0"}}`), 0o600))

	imp, err := jt.store.Imports().Create(ctx, store.NewImport{UserID: alice.ID, Origin: store.OriginNative, InboxFile: name})
	require.NoError(t, err)

	job, err := native.Job(alice.ID, imp.ID)
	require.NoError(t, err)

	_, err = jt.store.Jobs().Enqueue(ctx, job)
	require.NoError(t, err)

	claimed, err := jt.worker.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	got, err := jt.store.Imports().Get(ctx, alice.ID, imp.ID)
	require.NoError(t, err)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.NoFileExists(t, filepath.Join(jt.inboxDir, name))
}

// Given a native push of a sample, when the jobs of the server run, then the import.native job
// records its range and queues the aggregate.recompute job, which computes its aggregates.
func TestJobs_ImportNativeQueuesTheRecomputationOfTheAggregates(t *testing.T) {
	t.Parallel()

	jt := newJobsTest(t)
	ctx := t.Context()
	alice := jt.mustCreateUser(t, "alice", store.UserActive)

	name := "6b1e0c9d4f2a8e7c3b5d1a9f0e8c6b42"
	require.NoError(t, os.WriteFile(filepath.Join(jt.inboxDir, name), []byte(`{
		"format": "verve.ingest.v1",
		"client": {"name": "Verve for iOS", "version": "1.0.0"},
		"samples": [{"uuid": "2C7B5E1A-9D3F-4B6C-8E0A-1F2D3C4B5A69", "type": "HKQuantityTypeIdentifierStepCount",
		             "start": "2026-11-10T08:00:00+01:00", "end": "2026-11-10T08:10:00+01:00", "value": 1240, "unit": "count",
		             "source": {"name": "Apple Watch", "device": {"model": "Watch"}}}]
	}`), 0o600))

	imp, err := jt.store.Imports().Create(ctx, store.NewImport{UserID: alice.ID, Origin: store.OriginNative, InboxFile: name})
	require.NoError(t, err)

	job, err := native.Job(alice.ID, imp.ID)
	require.NoError(t, err)

	_, err = jt.store.Jobs().Enqueue(ctx, job)
	require.NoError(t, err)

	for range 2 {
		claimed, err := jt.worker.Tick(ctx)
		require.NoError(t, err)
		require.True(t, claimed)
	}

	steps, err := jt.store.MetricTypes().GetByKey(ctx, "step_count")
	require.NoError(t, err)

	days, err := jt.store.Aggregates().ListDaily(ctx, alice.ID, steps.ID, time.Unix(0, 0), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, days, 1)
	require.Equal(t, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), days[0].Start)
	require.InDelta(t, 1240, days[0].Sum, 0)
}
