package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// maxLostLeases is the number of lost leases after which a job is abandoned.
const maxLostLeases = 5

// jobs implements store.JobsRepository.
type jobs struct {
	q   queries
	clk clock.Clock
}

func (r jobs) Enqueue(ctx context.Context, job store.NewJob) (store.Job, error) {
	var row sqlcgen.Job

	now := r.clk.Now()
	if job.RunAt.IsZero() {
		job.RunAt = now
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.EnqueueJob(ctx, sqlcgen.EnqueueJobParams{
			Kind:      job.Kind,
			Payload:   string(job.Payload),
			RunAt:     toMillis(job.RunAt),
			UserID:    sql.NullInt64{Int64: job.UserID, Valid: job.UserID != 0},
			SerialKey: sql.NullString{String: job.SerialKey, Valid: job.SerialKey != ""},
			CreatedAt: toMillis(now),
		})

		return err
	})
	if err != nil {
		return store.Job{}, fmt.Errorf("enqueue job of kind %q: %w", job.Kind, err)
	}

	return jobOf(row), nil
}

func (r jobs) Get(ctx context.Context, id int64) (store.Job, error) {
	row, err := r.q.read().GetJob(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Job{}, fmt.Errorf("get job %d: %w", id, err)
	}

	return jobOf(row), nil
}

func (r jobs) Claim(ctx context.Context, lease time.Duration) (store.Job, error) {
	var (
		row     sqlcgen.Job
		claimed bool
	)

	// No claimed job is not an error of the write: it would roll back the abandoned jobs.
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		now := r.clk.Now()

		if err := q.CancelUnconfirmedJobs(ctx, nullMillis(now)); err != nil {
			return err
		}

		err := q.AbandonJobs(ctx, sqlcgen.AbandonJobsParams{
			LastError:     sql.NullString{String: fmt.Sprintf("abandoned after %d lost leases", maxLostLeases), Valid: true},
			Now:           nullMillis(now),
			MaxLostLeases: maxLostLeases,
		})
		if err != nil {
			return err
		}

		row, err = q.ClaimJob(ctx, sqlcgen.ClaimJobParams{LockedUntil: nullMillis(now.Add(lease)), Now: toMillis(now)})
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}

		if err != nil {
			return err
		}

		claimed = true

		return nil
	})
	if err != nil {
		return store.Job{}, fmt.Errorf("claim job: %w", err)
	}

	if !claimed {
		return store.Job{}, fmt.Errorf("claim job: %w", store.ErrNotFound)
	}

	return jobOf(row), nil
}

func (r jobs) Heartbeat(ctx context.Context, id, claims int64, lease time.Duration) (bool, error) {
	var cancelRequested bool

	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		requested, err := q.HeartbeatJob(ctx, sqlcgen.HeartbeatJobParams{
			LockedUntil: nullMillis(r.clk.Now().Add(lease)),
			ID:          id,
			Claims:      claims,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}

		if err != nil {
			return 0, err
		}

		cancelRequested = requested != 0

		return 1, nil
	})
	if err != nil {
		return false, fmt.Errorf("renew lease of job %d: %w", id, err)
	}

	return cancelRequested, nil
}

func (r jobs) Complete(ctx context.Context, id, claims int64) error {
	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		return q.CompleteJob(ctx, sqlcgen.CompleteJobParams{FinishedAt: nullMillis(r.clk.Now()), ID: id, Claims: claims})
	})
	if err != nil {
		return fmt.Errorf("complete job %d: %w", id, err)
	}

	return nil
}

func (r jobs) Fail(ctx context.Context, id, claims int64, message string) error {
	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		return q.FailJob(ctx, sqlcgen.FailJobParams{
			LastError:  sql.NullString{String: message, Valid: true},
			FinishedAt: nullMillis(r.clk.Now()),
			ID:         id,
			Claims:     claims,
		})
	})
	if err != nil {
		return fmt.Errorf("fail job %d: %w", id, err)
	}

	return nil
}

func (r jobs) Release(ctx context.Context, id, claims int64) error {
	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		return q.ReleaseJob(ctx, sqlcgen.ReleaseJobParams{ID: id, Claims: claims})
	})
	if err != nil {
		return fmt.Errorf("release job %d: %w", id, err)
	}

	return nil
}

func (r jobs) Retry(ctx context.Context, id, claims int64, message string, runAt time.Time) error {
	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		return q.RetryJob(ctx, sqlcgen.RetryJobParams{
			LastError: sql.NullString{String: message, Valid: true},
			RunAt:     toMillis(runAt),
			ID:        id,
			Claims:    claims,
		})
	})
	if err != nil {
		return fmt.Errorf("retry job %d: %w", id, err)
	}

	return nil
}

func (r jobs) Cancel(ctx context.Context, id int64) error {
	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		return q.CancelJob(ctx, sqlcgen.CancelJobParams{FinishedAt: nullMillis(r.clk.Now()), ID: id})
	})
	if err != nil {
		return fmt.Errorf("cancel job %d: %w", id, err)
	}

	return nil
}

func (r jobs) ConfirmCancel(ctx context.Context, id, claims int64) error {
	err := r.fenced(ctx, id, func(q *sqlcgen.Queries) (int64, error) {
		return q.ConfirmCancelJob(ctx, sqlcgen.ConfirmCancelJobParams{
			FinishedAt: nullMillis(r.clk.Now()),
			ID:         id,
			Claims:     claims,
		})
	})
	if err != nil {
		return fmt.Errorf("confirm cancellation of job %d: %w", id, err)
	}

	return nil
}

// fenced runs a write that applies only to a job id in the right state, usually running under its
// claims value. When it updates no row, it returns store.ErrNotFound for a missing job and
// store.ErrConflict otherwise.
func (r jobs) fenced(ctx context.Context, id int64, write func(q *sqlcgen.Queries) (int64, error)) error {
	return r.q.write(ctx, func(q *sqlcgen.Queries) error {
		updated, err := write(q)
		if err != nil {
			return err
		}

		if updated > 0 {
			return nil
		}

		_, err = q.GetJob(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		return store.ErrConflict
	})
}

func jobOf(row sqlcgen.Job) store.Job {
	return store.Job{
		ID:              row.ID,
		Kind:            row.Kind,
		Payload:         json.RawMessage(row.Payload),
		Status:          store.JobStatus(row.Status),
		Attempts:        row.Attempts,
		Claims:          row.Claims,
		LostLeases:      row.LostLeases,
		CancelRequested: row.CancelRequested != 0,
		RunAt:           fromMillis(row.RunAt),
		LockedUntil:     fromNullMillis(row.LockedUntil),
		UserID:          row.UserID.Int64,
		SerialKey:       row.SerialKey.String,
		LastError:       row.LastError.String,
		CreatedAt:       fromMillis(row.CreatedAt),
		FinishedAt:      fromNullMillis(row.FinishedAt),
	}
}
