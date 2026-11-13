package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// jobLease is the lease of every claim of the suite.
const jobLease = time.Minute

// jobsTime is the start of the fake clock of the suite.
var jobsTime = time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)

// dueJob returns a job due at runAt, with serialKey as its serial key.
func dueJob(runAt time.Time, serialKey string) store.NewJob {
	return store.NewJob{Kind: "test.job", Payload: json.RawMessage(`{}`), RunAt: runAt, SerialKey: serialKey}
}

func mustEnqueue(ctx context.Context, t *testing.T, jobs store.JobsRepository, job store.NewJob) store.Job {
	t.Helper()

	got, err := jobs.Enqueue(ctx, job)
	require.NoError(t, err)

	return got
}

func mustClaim(ctx context.Context, t *testing.T, jobs store.JobsRepository) store.Job {
	t.Helper()

	got, _, err := jobs.Claim(ctx, jobLease)
	require.NoError(t, err)

	return got
}

func mustGet(ctx context.Context, t *testing.T, jobs store.JobsRepository, id int64) store.Job {
	t.Helper()

	got, err := jobs.Get(ctx, id)
	require.NoError(t, err)

	return got
}

func requireNoClaim(ctx context.Context, t *testing.T, jobs store.JobsRepository) {
	t.Helper()

	_, _, err := jobs.Claim(ctx, jobLease)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// jobWrite is a write that the claims value of a claim must fence.
type jobWrite struct {
	name  string
	write func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error
	// want returns the job expected after the write, at time now, on the job returned by Claim.
	want func(claimed store.Job, now time.Time) store.Job
}

func jobWrites() []jobWrite {
	return []jobWrite{
		{
			name: "Heartbeat",
			write: func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error {
				_, err := jobs.Heartbeat(ctx, id, claims, 2*jobLease)

				return err
			},
			want: func(claimed store.Job, now time.Time) store.Job {
				claimed.LockedUntil = now.Add(2 * jobLease)

				return claimed
			},
		},
		{
			name: "Complete",
			write: func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error {
				return jobs.Complete(ctx, id, claims)
			},
			want: func(claimed store.Job, now time.Time) store.Job {
				claimed.Status = store.JobCompleted
				claimed.LockedUntil = time.Time{}
				claimed.FinishedAt = now

				return claimed
			},
		},
		{
			name: "Fail",
			write: func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error {
				return jobs.Fail(ctx, id, claims, "handler failed")
			},
			want: func(claimed store.Job, now time.Time) store.Job {
				claimed.Status = store.JobFailed
				claimed.Attempts = 1
				claimed.LastError = "handler failed"
				claimed.LockedUntil = time.Time{}
				claimed.FinishedAt = now

				return claimed
			},
		},
		{
			name: "Release",
			write: func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error {
				return jobs.Release(ctx, id, claims)
			},
			want: func(claimed store.Job, _ time.Time) store.Job {
				claimed.Status = store.JobQueued
				claimed.LockedUntil = time.Time{}

				return claimed
			},
		},
		{
			name: "Retry",
			write: func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error {
				return jobs.Retry(ctx, id, claims, "handler failed", jobsTime.Add(time.Hour))
			},
			want: func(claimed store.Job, _ time.Time) store.Job {
				claimed.Status = store.JobQueued
				claimed.Attempts = 1
				claimed.LastError = "handler failed"
				claimed.RunAt = jobsTime.Add(time.Hour)
				claimed.LockedUntil = time.Time{}

				return claimed
			},
		},
		{
			name: "ConfirmCancel",
			write: func(ctx context.Context, jobs store.JobsRepository, id, claims int64) error {
				return jobs.ConfirmCancel(ctx, id, claims)
			},
			want: func(claimed store.Job, now time.Time) store.Job {
				claimed.Status = store.JobCancelled
				claimed.LockedUntil = time.Time{}
				claimed.FinishedAt = now

				return claimed
			},
		},
	}
}

// RunJobs runs the JobsRepository contract against the stores returned by newStore.
func RunJobs(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runJobsEnqueue(t, newStore)
	runJobsClaim(t, newStore)
	runJobsSerialKeys(t, newStore)
	runJobsEnqueueClaimed(t, newStore)
	runJobsLostLeases(t, newStore)
	runJobsFencing(t, newStore)
	runJobsCancel(t, newStore)
	runJobsSchedules(t, newStore)
	runJobsActive(t, newStore)
	runJobsPurge(t, newStore)
	runJobsListByStatus(t, newStore)
	runJobsOfAUser(t, newStore)
}

// userJob returns a due job of the user of userID.
func userJob(userID int64) store.NewJob {
	job := dueJob(jobsTime, "")
	job.UserID = userID

	return job
}

func runJobsOfAUser(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	const alice, bob = 1, 2

	// Given queued, running and finished jobs of a user, then CancelByUser cancels the queued ones
	// at once, requests the cancellation of the running ones, which it counts, and leaves the
	// finished ones and the jobs of the other users as they are.
	t.Run("CancelByUserCancelsTheQueuedAndRunningJobsOfTheUser", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, userJob(alice))
		completed := mustClaim(ctx, t, jobs)
		require.NoError(t, jobs.Complete(ctx, completed.ID, completed.Claims))

		mustEnqueue(ctx, t, jobs, userJob(alice))
		running := mustClaim(ctx, t, jobs)
		queued := mustEnqueue(ctx, t, jobs, userJob(alice))
		other := mustEnqueue(ctx, t, jobs, userJob(bob))
		system := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))

		clk.Advance(time.Second)

		stillRunning, err := jobs.CancelByUser(ctx, alice)
		require.NoError(t, err)
		require.Equal(t, int64(1), stillRunning)

		got := mustGet(ctx, t, jobs, queued.ID)
		require.Equal(t, store.JobCancelled, got.Status)
		require.Equal(t, clk.Now(), got.FinishedAt)

		got = mustGet(ctx, t, jobs, running.ID)
		require.Equal(t, store.JobRunning, got.Status)
		require.True(t, got.CancelRequested)

		require.Equal(t, store.JobCompleted, mustGet(ctx, t, jobs, completed.ID).Status)
		require.Equal(t, other, mustGet(ctx, t, jobs, other.ID))
		require.Equal(t, system, mustGet(ctx, t, jobs, system.ID))

		require.NoError(t, jobs.ConfirmCancel(ctx, running.ID, running.Claims))

		stillRunning, err = jobs.CancelByUser(ctx, alice)
		require.NoError(t, err)
		require.Zero(t, stillRunning)
	})

	// Given jobs of two users, then DeleteByUser removes at most the limit of the jobs of one user,
	// whatever their status, and returns how many it removed, and the jobs of the other user stay.
	t.Run("DeleteByUserRemovesAtMostTheLimitOfTheJobsOfTheUser", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, userJob(alice))
		mustClaim(ctx, t, jobs)
		mustEnqueue(ctx, t, jobs, userJob(alice))
		mustEnqueue(ctx, t, jobs, userJob(alice))
		other := mustEnqueue(ctx, t, jobs, userJob(bob))

		deleted, err := jobs.DeleteByUser(ctx, alice, 2)
		require.NoError(t, err)
		require.Equal(t, int64(2), deleted)

		deleted, err = jobs.DeleteByUser(ctx, alice, 2)
		require.NoError(t, err)
		require.Equal(t, int64(1), deleted)

		deleted, err = jobs.DeleteByUser(ctx, alice, 2)
		require.NoError(t, err)
		require.Zero(t, deleted)

		require.Equal(t, other, mustGet(ctx, t, jobs, other.ID))

		_, err = jobs.DeleteByUser(ctx, alice, 0)
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}

// mustEnqueueClaimed stores a job without a serial key, running from now, and returns it.
func mustEnqueueClaimed(ctx context.Context, t *testing.T, jobs store.JobsRepository) store.Job {
	t.Helper()

	got, err := jobs.EnqueueClaimed(ctx, dueJob(jobsTime, ""), jobLease)
	require.NoError(t, err)

	return got
}

// listJobs returns the jobs that ListByStatus returns for status and page.
func listJobs(ctx context.Context, t *testing.T, jobs store.JobsRepository, status store.JobStatus, page store.PageRequest) []store.Job {
	t.Helper()

	got, err := jobs.ListByStatus(ctx, status, page)
	require.NoError(t, err)

	return got
}

// jobIDs returns the ids of jobs, in order.
func jobIDs(jobs ...store.Job) []int64 {
	ids := make([]int64, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}

	return ids
}

func runJobsListByStatus(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given jobs of every status, one of them created first and failed last and two failed at the
	// same time, then ListByStatus returns the jobs of the status asked for, newest first by
	// FinishedAt then ID, with every field.
	t.Run("ListByStatusReturnsTheJobsOfTheStatusNewestFirstByTheirEnd", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		failedLast := mustEnqueueClaimed(ctx, t, jobs)

		clk.Advance(time.Second)

		failedFirst := mustEnqueueClaimed(ctx, t, jobs)
		olderAtTheSameTime := mustEnqueueClaimed(ctx, t, jobs)
		newerAtTheSameTime := mustEnqueueClaimed(ctx, t, jobs)
		completed := mustEnqueueClaimed(ctx, t, jobs)
		cancelled := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(time.Hour), ""))
		mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(time.Hour), ""))
		mustEnqueueClaimed(ctx, t, jobs)

		clk.Advance(time.Second)
		require.NoError(t, jobs.Fail(ctx, failedFirst.ID, failedFirst.Claims, "handler failed"))
		require.NoError(t, jobs.Complete(ctx, completed.ID, completed.Claims))
		require.NoError(t, jobs.Cancel(ctx, cancelled.ID))

		clk.Advance(time.Second)
		require.NoError(t, jobs.Fail(ctx, newerAtTheSameTime.ID, newerAtTheSameTime.Claims, "handler failed"))
		require.NoError(t, jobs.Fail(ctx, olderAtTheSameTime.ID, olderAtTheSameTime.Claims, "handler failed"))

		clk.Advance(time.Second)
		require.NoError(t, jobs.Fail(ctx, failedLast.ID, failedLast.Claims, "handler failed"))

		got := listJobs(ctx, t, jobs, store.JobFailed, store.PageRequest{Limit: 10})
		require.Equal(t, jobIDs(failedLast, newerAtTheSameTime, olderAtTheSameTime, failedFirst), jobIDs(got...))
		require.Equal(t, mustGet(ctx, t, jobs, failedLast.ID), got[0])

		require.Equal(t, jobIDs(completed), jobIDs(listJobs(ctx, t, jobs, store.JobCompleted, store.PageRequest{Limit: 10})...))
		require.Equal(t, jobIDs(cancelled), jobIDs(listJobs(ctx, t, jobs, store.JobCancelled, store.PageRequest{Limit: 10})...))
	})

	// Given no job of a status, then ListByStatus returns none.
	t.Run("ListByStatusWithoutJobsOfTheStatusReturnsNone", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueueClaimed(ctx, t, jobs)

		require.Empty(t, listJobs(ctx, t, jobs, store.JobFailed, store.PageRequest{Limit: 10}))
	})

	// failEvery fails count running jobs, interval apart, and returns them as they were running.
	failEvery := func(ctx context.Context, t *testing.T, jobs store.JobsRepository, clk *clock.Fake, interval time.Duration, count int) []store.Job {
		t.Helper()

		failed := make([]store.Job, 0, count)

		for range count {
			clk.Advance(interval)

			job := mustEnqueueClaimed(ctx, t, jobs)
			require.NoError(t, jobs.Fail(ctx, job.ID, job.Claims, "handler failed"))

			failed = append(failed, job)
		}

		return failed
	}

	// Given more jobs than the limit, some of them ended at the same time, then ListByStatus returns
	// at most the limit, and following the position of the last job of each page lists every job
	// once, in order.
	t.Run("ListByStatusReturnsAtMostTheLimitAfterTheCursor", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		failed := slices.Concat(
			failEvery(ctx, t, jobs, clk, 0, 4),
			failEvery(ctx, t, jobs, clk, time.Second, 3),
		)

		var listed []store.Job

		page := store.PageRequest{Limit: 2}

		for range len(failed) {
			got := listJobs(ctx, t, jobs, store.JobFailed, page)
			require.LessOrEqual(t, len(got), 2)

			if len(got) == 0 {
				break
			}

			listed = append(listed, got...)
			page.After = got[len(got)-1].Cursor()
		}

		want := jobIDs(failed...)
		slices.Reverse(want)
		require.Equal(t, want, jobIDs(listed...))
	})

	// Given a page listed, when a job created before every listed job fails, then following the
	// cursor lists each job of the first list once and not the new one: it ended after them, so it
	// is newer than the cursor.
	t.Run("FollowingTheCursorListsEachJobOnceWhileJobsFail", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		late := mustEnqueueClaimed(ctx, t, jobs)
		failed := failEvery(ctx, t, jobs, clk, time.Second, 5)
		want := jobIDs(failed...)
		slices.Reverse(want)

		listed := listJobs(ctx, t, jobs, store.JobFailed, store.PageRequest{Limit: 2})
		require.Equal(t, want[:2], jobIDs(listed...))

		clk.Advance(time.Second)
		require.NoError(t, jobs.Fail(ctx, late.ID, late.Claims, "handler failed"))

		for range len(failed) {
			got := listJobs(ctx, t, jobs, store.JobFailed, store.PageRequest{After: listed[len(listed)-1].Cursor(), Limit: 2})
			if len(got) == 0 {
				break
			}

			listed = append(listed, got...)
		}

		require.Equal(t, want, jobIDs(listed...))
		require.Equal(t, late.ID, listJobs(ctx, t, jobs, store.JobFailed, store.PageRequest{Limit: 1})[0].ID)
	})

	// Given a limit below 1, then ListByStatus returns store.ErrInvalid.
	t.Run("ListByStatusWithALimitBelowOneReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		failEvery(ctx, t, jobs, clk, 0, 1)

		for _, limit := range []int{0, -1} {
			_, err := jobs.ListByStatus(ctx, store.JobFailed, store.PageRequest{Limit: limit})
			require.ErrorIs(t, err, store.ErrInvalid, "limit %d", limit)
		}
	})

	// Given a status that is not finished, queued, running or unknown, then ListByStatus returns
	// store.ErrInvalid: such jobs have no end to sort them by.
	t.Run("ListByStatusOfAnUnfinishedStatusReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(time.Hour), ""))
		mustEnqueueClaimed(ctx, t, jobs)

		for _, status := range []store.JobStatus{store.JobQueued, store.JobRunning, "paused"} {
			_, err := jobs.ListByStatus(ctx, status, store.PageRequest{Limit: 10})
			require.ErrorIs(t, err, store.ErrInvalid, "status %q", status)
		}
	})
}

func runJobsSchedules(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given no schedule, then ListSchedules returns none.
	t.Run("ListSchedulesWithoutSchedulesIsEmpty", func(t *testing.T) {
		t.Parallel()

		schedules, err := newStore(t, clock.NewFake(jobsTime)).Jobs().ListSchedules(t.Context())
		require.NoError(t, err)
		require.Empty(t, schedules)
	})

	// Given SetSchedule, then ListSchedules returns the next run of each name.
	t.Run("SetScheduleStoresTheNextRun", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		require.NoError(t, jobs.SetSchedule(ctx, "jobs.purge", jobsTime.Add(time.Hour)))
		require.NoError(t, jobs.SetSchedule(ctx, "backup.run", jobsTime))

		schedules, err := jobs.ListSchedules(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]time.Time{"jobs.purge": jobsTime.Add(time.Hour), "backup.run": jobsTime}, schedules)
	})

	// Given SetSchedule on a stored name, then its next run is replaced.
	t.Run("SetScheduleReplacesTheNextRun", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		require.NoError(t, jobs.SetSchedule(ctx, "jobs.purge", jobsTime))
		require.NoError(t, jobs.SetSchedule(ctx, "jobs.purge", jobsTime.Add(time.Hour)))

		schedules, err := jobs.ListSchedules(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]time.Time{"jobs.purge": jobsTime.Add(time.Hour)}, schedules)
	})

	// Given DeleteSchedule, then only the schedule of that name is removed.
	t.Run("DeleteScheduleRemovesTheSchedule", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		require.NoError(t, jobs.SetSchedule(ctx, "jobs.purge", jobsTime))
		require.NoError(t, jobs.SetSchedule(ctx, "backup.run", jobsTime))
		require.NoError(t, jobs.DeleteSchedule(ctx, "backup.run"))

		schedules, err := jobs.ListSchedules(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]time.Time{"jobs.purge": jobsTime}, schedules)
	})

	// Given DeleteSchedule on a missing name, then the error satisfies errors.Is(err, store.ErrNotFound).
	t.Run("DeleteScheduleOnAMissingNameReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		err := newStore(t, clock.NewFake(jobsTime)).Jobs().DeleteSchedule(t.Context(), "jobs.purge")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given SetSchedule through the repositories of WithTx, when the transaction rolls back, then the
	// next run is the one from before.
	t.Run("SetScheduleInARolledBackTransactionChangesNothing", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(jobsTime))
		ctx := t.Context()
		errRollback := errors.New("roll back")

		require.NoError(t, s.Jobs().SetSchedule(ctx, "jobs.purge", jobsTime))

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			if err := tx.Jobs().SetSchedule(ctx, "jobs.purge", jobsTime.Add(time.Hour)); err != nil {
				return err
			}

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		schedules, err := s.Jobs().ListSchedules(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]time.Time{"jobs.purge": jobsTime}, schedules)
	})
}

func runJobsActive(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	const key = "schedule:jobs.purge"

	// claimed enqueues a job of the serial key, and returns it claimed.
	claimed := func(ctx context.Context, t *testing.T, jobs store.JobsRepository) store.Job {
		t.Helper()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key))

		return mustClaim(ctx, t, jobs)
	}

	// Each prepare returns the id of the job of the serial key that is queued or running, or zero
	// when there is none.
	tests := []struct {
		name    string
		prepare func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64
	}{
		{
			name:    "NoJob",
			prepare: func(context.Context, *testing.T, store.JobsRepository) int64 { return 0 },
		},
		{
			name: "QueuedJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				return mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key)).ID
			},
		},
		{
			name: "JobWaitingForItsRetry",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				job := claimed(ctx, t, jobs)
				require.NoError(t, jobs.Retry(ctx, job.ID, job.Claims, "handler failed", jobsTime.Add(time.Hour)))

				return job.ID
			},
		},
		{
			name: "RunningJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				return claimed(ctx, t, jobs).ID
			},
		},
		{
			name: "CompletedJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				job := claimed(ctx, t, jobs)
				require.NoError(t, jobs.Complete(ctx, job.ID, job.Claims))

				return 0
			},
		},
		{
			name: "FailedJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				job := claimed(ctx, t, jobs)
				require.NoError(t, jobs.Fail(ctx, job.ID, job.Claims, "handler failed"))

				return 0
			},
		},
		{
			name: "CancelledJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				job := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key))
				require.NoError(t, jobs.Cancel(ctx, job.ID))

				return 0
			},
		},
		{
			name: "ActiveJobOfAnotherKey",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()
				mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "schedule:backup.run"))

				return 0
			},
		},
		{
			name: "FinishedJobAndQueuedJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				job := claimed(ctx, t, jobs)
				require.NoError(t, jobs.Complete(ctx, job.ID, job.Claims))

				return mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key)).ID
			},
		},
		{
			name: "RunningJobAndQueuedJob",
			prepare: func(ctx context.Context, t *testing.T, jobs store.JobsRepository) int64 {
				t.Helper()

				job := claimed(ctx, t, jobs)
				mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key))

				return job.ID
			},
		},
	}

	for _, tt := range tests {
		// Given jobs in a given state, then HasActiveJob reports whether one of the serial key is queued
		// or running.
		t.Run("HasActiveJobWith"+tt.name, func(t *testing.T) {
			t.Parallel()

			jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
			ctx := t.Context()

			want := tt.prepare(ctx, t, jobs)

			got, err := jobs.HasActiveJob(ctx, key)
			require.NoError(t, err)
			require.Equal(t, want != 0, got)
		})

		// Given jobs in a given state, then ActiveJob returns the oldest job of the serial key that is
		// queued or running, or ErrNotFound when there is none.
		t.Run("ActiveJobWith"+tt.name, func(t *testing.T) {
			t.Parallel()

			jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
			ctx := t.Context()

			want := tt.prepare(ctx, t, jobs)

			got, err := jobs.ActiveJob(ctx, key)
			if want == 0 {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
			require.Equal(t, mustGet(ctx, t, jobs, want), got)
		})
	}

	// Given jobs of the serial key of every state and a job of another key, then ActiveJobs returns
	// the jobs of the key that are queued or running, a job waiting for its retry included, oldest
	// first.
	t.Run("ActiveJobsReturnsTheQueuedAndRunningJobsOfTheKey", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		completed := claimed(ctx, t, jobs)
		require.NoError(t, jobs.Complete(ctx, completed.ID, completed.Claims))

		retrying := claimed(ctx, t, jobs)
		require.NoError(t, jobs.Retry(ctx, retrying.ID, retrying.Claims, "handler failed", jobsTime.Add(time.Hour)))

		running := claimed(ctx, t, jobs)
		queued := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key))

		cancelled := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key))
		require.NoError(t, jobs.Cancel(ctx, cancelled.ID))

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "schedule:backup.run"))

		got, err := jobs.ActiveJobs(ctx, key)
		require.NoError(t, err)
		require.Equal(t, []store.Job{
			mustGet(ctx, t, jobs, retrying.ID), mustGet(ctx, t, jobs, running.ID), mustGet(ctx, t, jobs, queued.ID),
		}, got)

		got, err = jobs.ActiveJobs(ctx, "schedule:cleanup.inbox")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	// Given HasActiveJob through the repositories of WithTx, then it sees a job of the transaction.
	t.Run("HasActiveJobSeesTheJobsOfItsTransaction", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(jobsTime))
		ctx := t.Context()

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustEnqueue(ctx, t, tx.Jobs(), dueJob(jobsTime, key))

			got, err := tx.Jobs().HasActiveJob(ctx, key)
			require.True(t, got)

			return err
		})
		require.NoError(t, err)
	})

	// Given ActiveJob through the repositories of WithTx, then it sees a job of the transaction.
	t.Run("ActiveJobSeesTheJobsOfItsTransaction", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(jobsTime))
		ctx := t.Context()

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			job := mustEnqueue(ctx, t, tx.Jobs(), dueJob(jobsTime, key))

			got, err := tx.Jobs().ActiveJob(ctx, key)
			require.NoError(t, err)
			require.Equal(t, job.ID, got.ID)

			return nil
		})
		require.NoError(t, err)
	})
}

func runJobsPurge(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// The limit of failed jobs comes first, because they are kept for longer.
	failedBefore := jobsTime.Add(time.Hour)
	completedBefore := jobsTime.Add(2 * time.Hour)

	complete := func(ctx context.Context, jobs store.JobsRepository, job store.Job) error {
		return jobs.Complete(ctx, job.ID, job.Claims)
	}
	cancel := func(ctx context.Context, jobs store.JobsRepository, job store.Job) error {
		return jobs.ConfirmCancel(ctx, job.ID, job.Claims)
	}
	fail := func(ctx context.Context, jobs store.JobsRepository, job store.Job) error {
		return jobs.Fail(ctx, job.ID, job.Claims, "handler failed")
	}

	tests := []struct {
		name       string
		end        func(ctx context.Context, jobs store.JobsRepository, job store.Job) error
		finishedAt time.Time
		removed    bool
	}{
		{name: "RemovesACompletedJobOlderThanItsLimit", end: complete, finishedAt: completedBefore.Add(-time.Millisecond), removed: true},
		{name: "KeepsACompletedJobAtItsLimit", end: complete, finishedAt: completedBefore},
		{name: "RemovesACancelledJobOlderThanItsLimit", end: cancel, finishedAt: completedBefore.Add(-time.Millisecond), removed: true},
		{name: "KeepsACancelledJobAtItsLimit", end: cancel, finishedAt: completedBefore},
		{name: "RemovesAFailedJobOlderThanItsLimit", end: fail, finishedAt: failedBefore.Add(-time.Millisecond), removed: true},
		{name: "KeepsAFailedJobAtItsLimit", end: fail, finishedAt: failedBefore},
	}

	for _, tt := range tests {
		// Given a finished job, then PurgeFinished removes it only if it ended before the limit of its
		// status.
		t.Run("PurgeFinished"+tt.name, func(t *testing.T) {
			t.Parallel()

			clk := clock.NewFake(jobsTime)
			jobs := newStore(t, clk).Jobs()
			ctx := t.Context()

			mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
			job := mustClaim(ctx, t, jobs)

			clk.Set(tt.finishedAt)
			require.NoError(t, tt.end(ctx, jobs, job))
			require.NoError(t, jobs.PurgeFinished(ctx, completedBefore, failedBefore))

			_, err := jobs.Get(ctx, job.ID)
			if tt.removed {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
		})
	}

	// Given queued and running jobs older than both limits, even with an expired lease, then
	// PurgeFinished keeps them.
	t.Run("PurgeFinishedKeepsQueuedAndRunningJobs", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		retried := mustClaim(ctx, t, jobs)
		require.NoError(t, jobs.Retry(ctx, retried.ID, retried.Claims, "handler failed", jobsTime.Add(time.Hour)))

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		running := mustClaim(ctx, t, jobs)
		queued := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))

		require.NoError(t, jobs.PurgeFinished(ctx, jobsTime.Add(24*time.Hour), jobsTime.Add(24*time.Hour)))

		require.Equal(t, store.JobQueued, mustGet(ctx, t, jobs, retried.ID).Status)
		require.Equal(t, store.JobRunning, mustGet(ctx, t, jobs, running.ID).Status)
		require.Equal(t, store.JobQueued, mustGet(ctx, t, jobs, queued.ID).Status)
	})
}

func runJobsEnqueue(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given Enqueue without a run_at, then the job is due from the clock's now, so that it does not
	// jump ahead of the jobs already waiting.
	t.Run("EnqueueWithoutRunAtIsDueFromNow", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()

		job := mustEnqueue(t.Context(), t, jobs, store.NewJob{Kind: "test.job", Payload: json.RawMessage(`{}`)})
		require.Equal(t, jobsTime, job.RunAt)
	})

	// Given Enqueue with a kind, a payload, a run_at and optional user id and serial key, then the
	// job is stored queued with 0 attempts and returned with its id.
	t.Run("EnqueueStoresTheJobQueuedWithNoAttempts", func(t *testing.T) {
		t.Parallel()

		runAt := jobsTime.Add(time.Hour)

		tests := []struct {
			name string
			job  store.NewJob
		}{
			{
				name: "WithUserAndSerialKey",
				job: store.NewJob{
					Kind:      "import.xml",
					Payload:   json.RawMessage(`{"file":"export.zip"}`),
					RunAt:     runAt,
					UserID:    7,
					SerialKey: "import:7",
				},
			},
			{
				name: "WithoutUserAndSerialKey",
				job: store.NewJob{
					Kind:    "cleanup.sessions",
					Payload: json.RawMessage(`{}`),
					RunAt:   runAt,
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
				ctx := t.Context()

				got := mustEnqueue(ctx, t, jobs, tt.job)
				require.NotZero(t, got.ID)
				require.Equal(t, store.Job{
					ID:        got.ID,
					Kind:      tt.job.Kind,
					Payload:   tt.job.Payload,
					Status:    store.JobQueued,
					RunAt:     runAt,
					UserID:    tt.job.UserID,
					SerialKey: tt.job.SerialKey,
					CreatedAt: jobsTime,
				}, got)

				require.Equal(t, got, mustGet(ctx, t, jobs, got.ID))
			})
		}
	})

	// Given Get on a missing id, then the error satisfies errors.Is(err, store.ErrNotFound).
	t.Run("GetOnMissingIDReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		_, err := newStore(t, clock.NewFake(jobsTime)).Jobs().Get(t.Context(), 1)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given Enqueue through the repositories of WithTx, when the transaction rolls back, then no job
	// exists.
	t.Run("EnqueueInARolledBackTransactionLeavesNoJob", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(jobsTime))
		ctx := t.Context()
		errRollback := errors.New("roll back")

		var id int64

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			job, err := tx.Jobs().Enqueue(ctx, dueJob(jobsTime, ""))
			if err != nil {
				return err
			}

			id = job.ID

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)
		require.NotZero(t, id)

		_, err = s.Jobs().Get(ctx, id)
		require.ErrorIs(t, err, store.ErrNotFound)
		requireNoClaim(ctx, t, s.Jobs())
	})
}

func runJobsClaim(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given several due jobs, then Claim returns the oldest by run_at then id, as running, with
	// locked_until at now plus the lease and claims incremented.
	t.Run("ClaimReturnsTheOldestDueJobAsRunning", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		third := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-time.Minute), ""))
		first := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-time.Hour), ""))
		second := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-time.Hour), ""))

		want := first
		want.Status = store.JobRunning
		want.Claims = 1
		want.LockedUntil = jobsTime.Add(jobLease)
		require.Equal(t, want, mustClaim(ctx, t, jobs))
		require.Equal(t, want, mustGet(ctx, t, jobs, first.ID))

		require.Equal(t, second.ID, mustClaim(ctx, t, jobs).ID)
		require.Equal(t, third.ID, mustClaim(ctx, t, jobs).ID)
		requireNoClaim(ctx, t, jobs)
	})

	// Given only jobs whose run_at is in the future, then Claim returns store.ErrNotFound, until
	// run_at is reached.
	t.Run("ClaimSkipsJobsDueInTheFuture", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		job := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(time.Hour), ""))

		requireNoClaim(ctx, t, jobs)
		require.Equal(t, job, mustGet(ctx, t, jobs, job.ID))

		clk.Advance(time.Hour)
		require.Equal(t, job.ID, mustClaim(ctx, t, jobs).ID)
	})

	// Given a running job whose lease has expired, then Claim returns it again, with attempts
	// unchanged and lost_leases incremented. Before it expires, the job is not claimed.
	t.Run("ClaimReturnsAJobWhoseLeaseHasExpired", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		claimed := mustClaim(ctx, t, jobs)

		clk.Advance(jobLease - time.Millisecond)
		requireNoClaim(ctx, t, jobs)

		clk.Advance(time.Millisecond)

		want := claimed
		want.Claims = 2
		want.LostLeases = 1
		want.LockedUntil = clk.Now().Add(jobLease)
		require.Equal(t, want, mustClaim(ctx, t, jobs))
	})
}

func runJobsSerialKeys(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a job waiting for its retry, then it does not hold its serial key: a newer job of the key
	// runs before it.
	t.Run("AJobWaitingForItsRetryDoesNotHoldItsSerialKey", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		older := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))
		newer := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))

		claimed := mustClaim(ctx, t, jobs)
		require.Equal(t, older.ID, claimed.ID)
		require.NoError(t, jobs.Retry(ctx, claimed.ID, claimed.Claims, "handler failed", jobsTime.Add(time.Minute)))

		require.Equal(t, newer.ID, mustClaim(ctx, t, jobs).ID)
	})

	// Given a running job with a valid lease and serial key K, then Claim skips queued jobs with key
	// K and returns the next job without it. Once the key is free, its queued job is claimed.
	t.Run("ClaimSkipsQueuedJobsWhoseSerialKeyIsHeld", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-3*time.Hour), "import:7"))
		sameKey := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-2*time.Hour), "import:7"))
		otherKey := mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-time.Hour), "import:8"))
		noKey := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))

		holder := mustClaim(ctx, t, jobs)

		require.Equal(t, otherKey.ID, mustClaim(ctx, t, jobs).ID)
		require.Equal(t, noKey.ID, mustClaim(ctx, t, jobs).ID)
		requireNoClaim(ctx, t, jobs)

		require.NoError(t, jobs.Complete(ctx, holder.ID, holder.Claims))
		require.Equal(t, sameKey.ID, mustClaim(ctx, t, jobs).ID)
	})

	// Given a running job of key K whose lease has expired and a queued job of key K with an older
	// run_at, then Claim resumes the running job first: its process may still be alive and able to
	// renew its lease, so a job of the same key must wait for the claims value to change.
	t.Run("ClaimResumesAnExpiredJobBeforeTheQueuedJobsOfItsSerialKey", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))
		holder := mustClaim(ctx, t, jobs)
		mustEnqueue(ctx, t, jobs, dueJob(jobsTime.Add(-time.Hour), "import:7"))

		clk.Advance(jobLease)

		require.Equal(t, holder.ID, mustClaim(ctx, t, jobs).ID)
		requireNoClaim(ctx, t, jobs)
	})
}

func runJobsEnqueueClaimed(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	const key = "import:7"

	// Given EnqueueClaimed on a free serial key, then the job is stored running, claimed once, with a
	// lease from now, and due from now whatever its run_at.
	t.Run("EnqueueClaimedStoresTheJobRunningWithALease", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		job := store.NewJob{
			Kind:      "import.xml",
			Payload:   json.RawMessage(`{"file":"export.zip"}`),
			RunAt:     jobsTime.Add(time.Hour),
			UserID:    7,
			SerialKey: key,
		}

		got, err := jobs.EnqueueClaimed(ctx, job, jobLease)
		require.NoError(t, err)
		require.NotZero(t, got.ID)
		require.Equal(t, store.Job{
			ID:          got.ID,
			Kind:        job.Kind,
			Payload:     job.Payload,
			Status:      store.JobRunning,
			Claims:      1,
			RunAt:       jobsTime,
			LockedUntil: jobsTime.Add(jobLease),
			UserID:      7,
			SerialKey:   key,
			CreatedAt:   jobsTime,
		}, got)

		require.Equal(t, got, mustGet(ctx, t, jobs, got.ID))
	})

	// claimed enqueues a job of serialKey, and returns it claimed.
	claimed := func(ctx context.Context, t *testing.T, jobs store.JobsRepository, serialKey string) store.Job {
		t.Helper()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, serialKey))

		return mustClaim(ctx, t, jobs)
	}

	tests := []struct {
		name    string
		prepare func(ctx context.Context, t *testing.T, clk *clock.Fake, jobs store.JobsRepository)
		held    bool
	}{
		{
			name:    "NoJob",
			prepare: func(context.Context, *testing.T, *clock.Fake, store.JobsRepository) {},
		},
		{
			name: "RunningJob",
			prepare: func(ctx context.Context, t *testing.T, _ *clock.Fake, jobs store.JobsRepository) {
				t.Helper()
				claimed(ctx, t, jobs, key)
			},
			held: true,
		},
		{
			// Its process can still renew the lease, until another claim changes its claims value.
			name: "RunningJobWhoseLeaseHasExpired",
			prepare: func(ctx context.Context, t *testing.T, clk *clock.Fake, jobs store.JobsRepository) {
				t.Helper()
				claimed(ctx, t, jobs, key)
				clk.Advance(jobLease)
			},
			held: true,
		},
		{
			// Claim would take it before a newer job of the key, in arrival order.
			name: "DueQueuedJob",
			prepare: func(ctx context.Context, t *testing.T, _ *clock.Fake, jobs store.JobsRepository) {
				t.Helper()
				mustEnqueue(ctx, t, jobs, dueJob(jobsTime, key))
			},
			held: true,
		},
		{
			name: "JobWaitingForItsRetry",
			prepare: func(ctx context.Context, t *testing.T, _ *clock.Fake, jobs store.JobsRepository) {
				t.Helper()

				job := claimed(ctx, t, jobs, key)
				require.NoError(t, jobs.Retry(ctx, job.ID, job.Claims, "handler failed", jobsTime.Add(time.Hour)))
			},
		},
		{
			name: "CompletedJob",
			prepare: func(ctx context.Context, t *testing.T, _ *clock.Fake, jobs store.JobsRepository) {
				t.Helper()

				job := claimed(ctx, t, jobs, key)
				require.NoError(t, jobs.Complete(ctx, job.ID, job.Claims))
			},
		},
		{
			name: "RunningJobOfAnotherKey",
			prepare: func(ctx context.Context, t *testing.T, _ *clock.Fake, jobs store.JobsRepository) {
				t.Helper()
				claimed(ctx, t, jobs, "import:8")
			},
		},
	}

	for _, tt := range tests {
		// Given jobs in a given state, then EnqueueClaimed returns store.ErrConflict when a running job
		// or a due queued job holds the serial key, and otherwise the job running.
		t.Run("EnqueueClaimedWith"+tt.name, func(t *testing.T) {
			t.Parallel()

			clk := clock.NewFake(jobsTime)
			jobs := newStore(t, clk).Jobs()
			ctx := t.Context()

			tt.prepare(ctx, t, clk, jobs)

			got, err := jobs.EnqueueClaimed(ctx, dueJob(jobsTime, key), jobLease)
			if tt.held {
				require.ErrorIs(t, err, store.ErrConflict)

				return
			}

			require.NoError(t, err)
			require.Equal(t, store.JobRunning, got.Status)
		})
	}

	// Given a held serial key, then EnqueueClaimed leaves no job behind: once the key is free, there
	// is nothing to claim.
	t.Run("EnqueueClaimedOnAHeldKeyLeavesNoJob", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		holder := claimed(ctx, t, jobs, key)

		_, err := jobs.EnqueueClaimed(ctx, dueJob(jobsTime, key), jobLease)
		require.ErrorIs(t, err, store.ErrConflict)

		require.NoError(t, jobs.Complete(ctx, holder.ID, holder.Claims))
		requireNoClaim(ctx, t, jobs)
	})

	// Given a job without a serial key, then the running jobs without one do not hold it back.
	t.Run("EnqueueClaimedWithoutASerialKeyClaimsTheJob", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		claimed(ctx, t, jobs, "")

		got, err := jobs.EnqueueClaimed(ctx, dueJob(jobsTime, ""), jobLease)
		require.NoError(t, err)
		require.Equal(t, store.JobRunning, got.Status)
	})
}

func runJobsLostLeases(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// loseFiveLeases claims the only due job 5 times, letting each lease expire, and returns the job
	// as the last claim left it.
	loseFiveLeases := func(ctx context.Context, t *testing.T, clk *clock.Fake, jobs store.JobsRepository) store.Job {
		t.Helper()

		var claimed store.Job

		for lost := range 5 {
			claimed = mustClaim(ctx, t, jobs)
			require.Equal(t, int64(lost), claimed.LostLeases)

			clk.Advance(jobLease)
		}

		return claimed
	}

	// abandoned returns the job that lost its lease for the fifth time, as Claim abandons it at now.
	abandoned := func(claimed store.Job, now time.Time) store.Job {
		want := claimed
		want.Status = store.JobFailed
		want.LostLeases = 5
		want.LastError = "abandoned after 5 lost leases"
		want.LockedUntil = time.Time{}
		want.FinishedAt = now

		return want
	}

	// Given a job that lost its lease 5 times, when it would be claimed again, then it is marked
	// failed with the error "abandoned after 5 lost leases", and Claim returns it as abandoned
	// along with ErrNotFound, once.
	t.Run("ClaimAbandonsAJobThatLostItsLeaseFiveTimes", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		claimed := loseFiveLeases(ctx, t, clk, jobs)

		_, gotAbandoned, err := jobs.Claim(ctx, jobLease)
		require.ErrorIs(t, err, store.ErrNotFound)

		want := abandoned(claimed, clk.Now())
		require.Equal(t, []store.Job{want}, gotAbandoned)
		require.Equal(t, want, mustGet(ctx, t, jobs, claimed.ID))

		_, gotAbandoned, err = jobs.Claim(ctx, jobLease)
		require.ErrorIs(t, err, store.ErrNotFound)
		require.Empty(t, gotAbandoned)
	})

	// Given an abandoned job, then Claim goes on with the next job, returns the abandoned job with
	// it, and the serial key of the abandoned job is free.
	t.Run("ClaimReturnsTheNextJobAfterAbandoningOne", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))
		claimed := loseFiveLeases(ctx, t, clk, jobs)
		next := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))

		got, gotAbandoned, err := jobs.Claim(ctx, jobLease)
		require.NoError(t, err)
		require.Equal(t, next.ID, got.ID)
		require.Equal(t, []store.Job{abandoned(claimed, clk.Now())}, gotAbandoned)
		require.Equal(t, store.JobFailed, mustGet(ctx, t, jobs, claimed.ID).Status)
	})

	// Given Release, then the job is claimed again without counting a lost lease.
	t.Run("ReleasedJobIsClaimedWithoutCountingALostLease", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		claimed := mustClaim(ctx, t, jobs)

		require.NoError(t, jobs.Release(ctx, claimed.ID, claimed.Claims))

		again := mustClaim(ctx, t, jobs)
		require.Equal(t, claimed.ID, again.ID)
		require.Equal(t, int64(2), again.Claims)
		require.Zero(t, again.LostLeases)
	})
}

func runJobsFencing(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	for _, tt := range jobWrites() {
		// Given a write with the claims value of the claim, then it applies.
		t.Run(tt.name+"WithTheClaimsValueOfTheClaimApplies", func(t *testing.T) {
			t.Parallel()

			clk := clock.NewFake(jobsTime)
			jobs := newStore(t, clk).Jobs()
			ctx := t.Context()

			mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
			claimed := mustClaim(ctx, t, jobs)

			clk.Advance(10 * time.Second)
			require.NoError(t, tt.write(ctx, jobs, claimed.ID, claimed.Claims))

			require.Equal(t, tt.want(claimed, clk.Now()), mustGet(ctx, t, jobs, claimed.ID))
		})

		// Given a stale claims value, then the write returns store.ErrConflict and changes nothing.
		t.Run(tt.name+"WithAStaleClaimsValueConflicts", func(t *testing.T) {
			t.Parallel()

			clk := clock.NewFake(jobsTime)
			jobs := newStore(t, clk).Jobs()
			ctx := t.Context()

			mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
			stale := mustClaim(ctx, t, jobs)

			clk.Advance(jobLease)
			current := mustClaim(ctx, t, jobs)

			err := tt.write(ctx, jobs, stale.ID, stale.Claims)
			require.ErrorIs(t, err, store.ErrConflict)
			require.Equal(t, current, mustGet(ctx, t, jobs, stale.ID))
		})

		// Given a job that is no longer running, then the claims value of its last claim is stale.
		t.Run(tt.name+"OnAFinishedJobConflicts", func(t *testing.T) {
			t.Parallel()

			jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
			ctx := t.Context()

			mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
			claimed := mustClaim(ctx, t, jobs)

			require.NoError(t, jobs.Complete(ctx, claimed.ID, claimed.Claims))
			finished := mustGet(ctx, t, jobs, claimed.ID)

			err := tt.write(ctx, jobs, claimed.ID, claimed.Claims)
			require.ErrorIs(t, err, store.ErrConflict)
			require.Equal(t, finished, mustGet(ctx, t, jobs, claimed.ID))
		})

		// Given a missing job, then the write returns store.ErrNotFound.
		t.Run(tt.name+"OnAMissingJobReturnsErrNotFound", func(t *testing.T) {
			t.Parallel()

			jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()

			err := tt.write(t.Context(), jobs, 1, 1)
			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}

	// Given a job retried with a later run_at, then it is not due before that time.
	t.Run("RetryKeepsTheJobUntilItsRunAt", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		claimed := mustClaim(ctx, t, jobs)

		require.NoError(t, jobs.Retry(ctx, claimed.ID, claimed.Claims, "handler failed", jobsTime.Add(2*jobLease)))

		clk.Advance(jobLease)
		requireNoClaim(ctx, t, jobs)

		clk.Advance(jobLease)
		require.Equal(t, claimed.ID, mustClaim(ctx, t, jobs).ID)
	})
}

func runJobsCancel(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a queued job, when it is cancelled, then it is cancelled at once and never claimed.
	t.Run("CancelOnAQueuedJobCancelsItAtOnce", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		job := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))

		clk.Advance(time.Second)
		require.NoError(t, jobs.Cancel(ctx, job.ID))

		got := mustGet(ctx, t, jobs, job.ID)
		require.Equal(t, store.JobCancelled, got.Status)
		require.Equal(t, clk.Now(), got.FinishedAt)
		requireNoClaim(ctx, t, jobs)
	})

	// Given a running job, when it is cancelled, then it keeps running with the request recorded,
	// and only the heartbeats after the request report it.
	t.Run("CancelOnARunningJobRequestsIt", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		claimed := mustClaim(ctx, t, jobs)

		requested, err := jobs.Heartbeat(ctx, claimed.ID, claimed.Claims, jobLease)
		require.NoError(t, err)
		require.False(t, requested)

		require.NoError(t, jobs.Cancel(ctx, claimed.ID))

		got := mustGet(ctx, t, jobs, claimed.ID)
		require.Equal(t, store.JobRunning, got.Status)
		require.True(t, got.CancelRequested)

		requested, err = jobs.Heartbeat(ctx, claimed.ID, claimed.Claims, jobLease)
		require.NoError(t, err)
		require.True(t, requested)
	})

	// Given a finished job, when it is cancelled, then store.ErrConflict is returned and nothing
	// changes.
	for name, finish := range map[string]func(ctx context.Context, jobs store.JobsRepository, job store.Job) error{
		"Completed": func(ctx context.Context, jobs store.JobsRepository, job store.Job) error {
			return jobs.Complete(ctx, job.ID, job.Claims)
		},
		"Failed": func(ctx context.Context, jobs store.JobsRepository, job store.Job) error {
			return jobs.Fail(ctx, job.ID, job.Claims, "handler failed")
		},
		"Cancelled": func(ctx context.Context, jobs store.JobsRepository, job store.Job) error {
			return jobs.ConfirmCancel(ctx, job.ID, job.Claims)
		},
	} {
		t.Run("CancelOnA"+name+"JobConflicts", func(t *testing.T) {
			t.Parallel()

			jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()
			ctx := t.Context()

			mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
			claimed := mustClaim(ctx, t, jobs)

			require.NoError(t, finish(ctx, jobs, claimed))
			finished := mustGet(ctx, t, jobs, claimed.ID)

			require.ErrorIs(t, jobs.Cancel(ctx, claimed.ID), store.ErrConflict)
			require.Equal(t, finished, mustGet(ctx, t, jobs, claimed.ID))
		})
	}

	// Given a missing job, then Cancel returns store.ErrNotFound.
	t.Run("CancelOnAMissingJobReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		jobs := newStore(t, clock.NewFake(jobsTime)).Jobs()

		require.ErrorIs(t, jobs.Cancel(t.Context(), 1), store.ErrNotFound)
	})

	// Given a cancellation that its handler could not confirm, because the job went back to queued
	// or its process died, then the next Claim cancels the job instead of running it.
	for name, end := range map[string]func(ctx context.Context, jobs store.JobsRepository, clk *clock.Fake, job store.Job) error{
		"Retried": func(ctx context.Context, jobs store.JobsRepository, _ *clock.Fake, job store.Job) error {
			return jobs.Retry(ctx, job.ID, job.Claims, "handler failed", jobsTime)
		},
		"Released": func(ctx context.Context, jobs store.JobsRepository, _ *clock.Fake, job store.Job) error {
			return jobs.Release(ctx, job.ID, job.Claims)
		},
		"LeaseExpired": func(_ context.Context, _ store.JobsRepository, clk *clock.Fake, _ store.Job) error {
			clk.Advance(jobLease)

			return nil
		},
	} {
		t.Run("ClaimCancelsA"+name+"JobWhoseCancellationWasRequested", func(t *testing.T) {
			t.Parallel()

			clk := clock.NewFake(jobsTime)
			jobs := newStore(t, clk).Jobs()
			ctx := t.Context()

			mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
			claimed := mustClaim(ctx, t, jobs)

			require.NoError(t, jobs.Cancel(ctx, claimed.ID))
			require.NoError(t, end(ctx, jobs, clk, claimed))
			requireNoClaim(ctx, t, jobs)

			got := mustGet(ctx, t, jobs, claimed.ID)
			require.Equal(t, store.JobCancelled, got.Status)
			require.Equal(t, clk.Now(), got.FinishedAt)
			require.True(t, got.LockedUntil.IsZero())
		})
	}
}
