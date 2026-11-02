package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

const day = 24 * time.Hour

// Given a finished job, then the purge removes it once it is older than 7 days, or 30 days when it
// failed.
func TestPurge_RemovesFinishedJobsOlderThanTheirRetention(t *testing.T) {
	t.Parallel()

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
		name    string
		end     func(ctx context.Context, jobs store.JobsRepository, job store.Job) error
		age     time.Duration
		removed bool
	}{
		{name: "CompletedFor7Days", end: complete, age: 7 * day},
		{name: "CompletedForMoreThan7Days", end: complete, age: 7*day + time.Millisecond, removed: true},
		{name: "CancelledFor7Days", end: cancel, age: 7 * day},
		{name: "CancelledForMoreThan7Days", end: cancel, age: 7*day + time.Millisecond, removed: true},
		{name: "FailedFor30Days", end: fail, age: 30 * day},
		{name: "FailedForMoreThan30Days", end: fail, age: 30*day + time.Millisecond, removed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			jobs := s.Jobs()

			_, err := jobs.Enqueue(ctx, store.NewJob{Kind: "test.kind", Payload: json.RawMessage(`{}`)})
			require.NoError(t, err)

			job, err := jobs.Claim(ctx, time.Minute)
			require.NoError(t, err)
			require.NoError(t, tt.end(ctx, jobs, job))

			clk.Advance(tt.age)
			require.NoError(t, Purge(ctx, jobs, clk.Now()))

			_, err = jobs.Get(ctx, job.ID)
			if tt.removed {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
		})
	}
}

// Given queued and running jobs older than every retention, then the purge never removes them.
func TestPurge_KeepsQueuedAndRunningJobs(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	jobs := s.Jobs()

	_, err := jobs.Enqueue(ctx, store.NewJob{Kind: "test.kind", Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)

	running, err := jobs.Claim(ctx, time.Minute)
	require.NoError(t, err)

	queued, err := jobs.Enqueue(ctx, store.NewJob{Kind: "test.kind", Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)

	clk.Advance(365 * day)
	require.NoError(t, Purge(ctx, jobs, clk.Now()))

	require.Equal(t, store.JobRunning, getJob(ctx, t, s, running.ID).Status)
	require.Equal(t, store.JobQueued, getJob(ctx, t, s, queued.ID).Status)
}
