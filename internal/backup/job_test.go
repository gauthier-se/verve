package backup_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// scheduledArchive is the name of the archive of the backup.run job of the fixture.
const scheduledArchive = "verve-1.2.3-20261110T140305Z-scheduled.tar.zst"

// writeArchives creates empty files named like archives in dir.
func writeArchives(t *testing.T, dir string, names ...string) {
	t.Helper()

	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}
}

// runBackupJob registers the backup.run kind with s and keep, enqueues one job of it, runs it with
// a worker and returns the job as it ended.
func runBackupJob(t *testing.T, f fixture, s store.Store, keep int) store.Job {
	t.Helper()

	registry := jobs.NewRegistry()
	backup.Register(registry, f.backups, s, keep)

	queued, err := f.store.Jobs().Enqueue(t.Context(), store.NewJob{Kind: backup.KindRun, Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)

	worker := jobs.NewWorker(f.store, registry, clock.Real(), slog.New(slog.DiscardHandler), jobs.Options{})

	claimed, err := worker.Tick(t.Context())
	require.NoError(t, err)
	require.True(t, claimed)

	job, err := f.store.Jobs().Get(t.Context(), queued.ID)
	require.NoError(t, err)

	return job
}

// Given a successful scheduled backup, then the job completes and only the last keep scheduled
// backups stay, the new one included. Manual and pre-migration backups stay whatever their age.
func TestRunJob_KeepsTheLastScheduledBackups(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	writeArchives(t, f.config.Dir,
		"verve-1.2.3-20261107T030000Z-scheduled.tar.zst",
		"verve-1.2.3-20261108T030000Z-scheduled.tar.zst",
		"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
		"verve-1.2.0-20261001T120000Z-manual.tar.zst",
		"verve-1.1.0-20260901T120000Z-pre-migration.tar.zst",
	)

	job := runBackupJob(t, f, f.store, 2)
	require.Equal(t, store.JobCompleted, job.Status)

	require.ElementsMatch(t, []string{
		"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
		scheduledArchive,
		"verve-1.2.0-20261001T120000Z-manual.tar.zst",
		"verve-1.1.0-20260901T120000Z-pre-migration.tar.zst",
	}, dirNames(t, f.config.Dir))

	openArchivedDB(t, readArchive(t, filepath.Join(f.config.Dir, scheduledArchive)))
}

// Given a failed backup, then the job is failed at its first attempt with the error, for admins to
// see, and retention does not run.
func TestRunJob_FailedBackupFailsTheJobWithoutRetention(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	older := []string{
		"verve-1.2.3-20261107T030000Z-scheduled.tar.zst",
		"verve-1.2.3-20261108T030000Z-scheduled.tar.zst",
		"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
	}
	writeArchives(t, f.config.Dir, older...)

	job := runBackupJob(t, f, failingStore{Store: f.store, snapshotErr: errors.New("disk on fire")}, 1)
	require.Equal(t, store.JobFailed, job.Status)
	require.Equal(t, int64(1), job.Attempts)
	require.Contains(t, job.LastError, "disk on fire")

	require.ElementsMatch(t, older, dirNames(t, f.config.Dir))
}

// Given a backup run, then the audit event is recorded with the system as actor and the scheduled
// reason.
func TestRunJob_RecordsTheAuditEventWithTheSystemAsActor(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	job := runBackupJob(t, f, f.store, 7)
	require.Equal(t, store.JobCompleted, job.Status)

	events, err := f.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "backup.created", events[0].Type)
	require.Zero(t, events[0].ActorID)
	require.JSONEq(t, `{"reason":"scheduled","file":"`+scheduledArchive+`"}`, string(events[0].Payload))
}
