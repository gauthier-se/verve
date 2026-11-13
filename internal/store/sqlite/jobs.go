package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func (r jobs) Claim(ctx context.Context, lease time.Duration) (store.Job, []store.Job, error) {
	var (
		row       sqlcgen.Job
		abandoned []sqlcgen.Job
		claimed   bool
	)

	// No claimed job is not an error of the write: it would roll back the abandoned jobs.
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		now := r.clk.Now()

		if err := q.CancelUnconfirmedJobs(ctx, nullMillis(now)); err != nil {
			return err
		}

		var err error

		abandoned, err = q.AbandonJobs(ctx, sqlcgen.AbandonJobsParams{
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
		return store.Job{}, nil, fmt.Errorf("claim job: %w", err)
	}

	var abandonedJobs []store.Job
	for _, a := range abandoned {
		abandonedJobs = append(abandonedJobs, jobOf(a))
	}

	if !claimed {
		return store.Job{}, abandonedJobs, fmt.Errorf("claim job: %w", store.ErrNotFound)
	}

	return jobOf(row), abandonedJobs, nil
}

func (r jobs) EnqueueClaimed(ctx context.Context, job store.NewJob, lease time.Duration) (store.Job, error) {
	var row sqlcgen.Job

	serialKey := sql.NullString{String: job.SerialKey, Valid: job.SerialKey != ""}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		now := r.clk.Now()

		held, err := q.SerialKeyHeld(ctx, sqlcgen.SerialKeyHeldParams{SerialKey: serialKey, Now: toMillis(now)})
		if err != nil {
			return err
		}

		if held {
			return store.ErrConflict
		}

		row, err = q.EnqueueClaimedJob(ctx, sqlcgen.EnqueueClaimedJobParams{
			Kind:        job.Kind,
			Payload:     string(job.Payload),
			RunAt:       toMillis(now),
			LockedUntil: nullMillis(now.Add(lease)),
			UserID:      sql.NullInt64{Int64: job.UserID, Valid: job.UserID != 0},
			SerialKey:   serialKey,
			CreatedAt:   toMillis(now),
		})

		return err
	})
	if err != nil {
		return store.Job{}, fmt.Errorf("enqueue claimed job of kind %q: %w", job.Kind, err)
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

func (r jobs) ListSchedules(ctx context.Context) (map[string]time.Time, error) {
	rows, err := r.q.read().ListJobSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list job schedules: %w", err)
	}

	schedules := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		schedules[row.Name] = fromMillis(row.NextRun)
	}

	return schedules, nil
}

func (r jobs) SetSchedule(ctx context.Context, name string, nextRun time.Time) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.SetJobSchedule(ctx, sqlcgen.SetJobScheduleParams{Name: name, NextRun: toMillis(nextRun)})
	})
	if err != nil {
		return fmt.Errorf("set job schedule %q: %w", name, err)
	}

	return nil
}

func (r jobs) DeleteSchedule(ctx context.Context, name string) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		deleted, err := q.DeleteJobSchedule(ctx, name)
		if err != nil {
			return err
		}

		if deleted == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("delete job schedule %q: %w", name, err)
	}

	return nil
}

func (r jobs) HasActiveJob(ctx context.Context, serialKey string) (bool, error) {
	active, err := r.q.read().HasActiveJob(ctx, sql.NullString{String: serialKey, Valid: true})
	if err != nil {
		return false, fmt.Errorf("find active job of serial key %q: %w", serialKey, err)
	}

	return active, nil
}

func (r jobs) ActiveJob(ctx context.Context, serialKey string) (store.Job, error) {
	row, err := r.q.read().ActiveJob(ctx, sql.NullString{String: serialKey, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Job{}, fmt.Errorf("get active job of serial key %q: %w", serialKey, err)
	}

	return jobOf(row), nil
}

func (r jobs) ActiveJobs(ctx context.Context, serialKey string) ([]store.Job, error) {
	rows, err := r.q.read().ActiveJobs(ctx, sql.NullString{String: serialKey, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list active jobs of serial key %q: %w", serialKey, err)
	}

	active := make([]store.Job, 0, len(rows))
	for _, row := range rows {
		active = append(active, jobOf(row))
	}

	return active, nil
}

func (r jobs) PurgeFinished(ctx context.Context, completedBefore, failedBefore time.Time) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.PurgeFinishedJobs(ctx, sqlcgen.PurgeFinishedJobsParams{
			CompletedBefore: nullMillis(completedBefore),
			FailedBefore:    nullMillis(failedBefore),
		})
	})
	if err != nil {
		return fmt.Errorf("purge finished jobs: %w", err)
	}

	return nil
}

func (r jobs) CancelByUser(ctx context.Context, userID int64) (int64, error) {
	owner := sql.NullInt64{Int64: userID, Valid: true}

	var running int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		err := q.CancelJobsOfUser(ctx, sqlcgen.CancelJobsOfUserParams{FinishedAt: nullMillis(r.clk.Now()), UserID: owner})
		if err != nil {
			return err
		}

		running, err = q.CountRunningJobsOfUser(ctx, owner)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("cancel jobs of user %d: %w", userID, err)
	}

	return running, nil
}

func (r jobs) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete jobs of user %d: limit %d below 1: %w", userID, limit, store.ErrInvalid)
	}

	var deleted int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		deleted, err = q.DeleteJobsOfUser(ctx, sqlcgen.DeleteJobsOfUserParams{
			UserID:    sql.NullInt64{Int64: userID, Valid: true},
			BatchSize: int64(limit),
		})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete jobs of user %d: %w", userID, err)
	}

	return deleted, nil
}

func (r jobs) ListByStatus(ctx context.Context, status store.JobStatus, page store.PageRequest) ([]store.Job, error) {
	if page.Limit < 1 {
		return nil, fmt.Errorf("list %s jobs: limit %d below 1: %w", status, page.Limit, store.ErrInvalid)
	}

	if !status.Finished() {
		return nil, fmt.Errorf("list %s jobs: not a finished status: %w", status, store.ErrInvalid)
	}

	// The position before every job, so that the query always has one.
	params := sqlcgen.ListJobsByStatusParams{
		Status:  string(status),
		AfterAt: sql.NullInt64{Int64: math.MaxInt64, Valid: true},
		AfterID: math.MaxInt64,
		MaxJobs: int64(page.Limit),
	}

	if page.After != (store.Cursor{}) {
		params.AfterAt = nullMillis(page.After.At)
		params.AfterID = page.After.ID
	}

	rows, err := r.q.read().ListJobsByStatus(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list %s jobs: %w", status, err)
	}

	listed := make([]store.Job, 0, len(rows))
	for _, row := range rows {
		listed = append(listed, jobOf(row))
	}

	return listed, nil
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
