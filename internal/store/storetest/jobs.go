package storetest

import (
	"context"
	"encoding/json"
	"errors"
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

	got, err := jobs.Claim(ctx, jobLease)
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

	_, err := jobs.Claim(ctx, jobLease)
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
				return jobs.Heartbeat(ctx, id, claims, 2*jobLease)
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
	}
}

// RunJobs runs the JobsRepository contract against the stores returned by newStore.
func RunJobs(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runJobsEnqueue(t, newStore)
	runJobsClaim(t, newStore)
	runJobsSerialKeys(t, newStore)
	runJobsLostLeases(t, newStore)
	runJobsFencing(t, newStore)
}

func runJobsEnqueue(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

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

	// Given a job that lost its lease 5 times, when it would be claimed again, then it is marked
	// failed with the error "abandoned after 5 lost leases".
	t.Run("ClaimAbandonsAJobThatLostItsLeaseFiveTimes", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, ""))
		claimed := loseFiveLeases(ctx, t, clk, jobs)

		requireNoClaim(ctx, t, jobs)

		want := claimed
		want.Status = store.JobFailed
		want.LostLeases = 5
		want.LastError = "abandoned after 5 lost leases"
		want.LockedUntil = time.Time{}
		want.FinishedAt = clk.Now()
		require.Equal(t, want, mustGet(ctx, t, jobs, claimed.ID))
	})

	// Given an abandoned job, then Claim goes on with the next job, and the serial key of the
	// abandoned job is free.
	t.Run("ClaimReturnsTheNextJobAfterAbandoningOne", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(jobsTime)
		jobs := newStore(t, clk).Jobs()
		ctx := t.Context()

		mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))
		claimed := loseFiveLeases(ctx, t, clk, jobs)
		next := mustEnqueue(ctx, t, jobs, dueJob(jobsTime, "import:7"))

		require.Equal(t, next.ID, mustClaim(ctx, t, jobs).ID)
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
}
