package store

import (
	"context"
	"encoding/json"
	"time"
)

// JobStatus is the state of a job.
type JobStatus string

// The statuses of a job.
const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// Finished reports whether s is the status of a job that ended: completed, failed or cancelled.
func (s JobStatus) Finished() bool {
	switch s {
	case JobCompleted, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

// NewJob describes a job to enqueue.
type NewJob struct {
	Kind    string
	Payload json.RawMessage
	// RunAt is the earliest time the job can be claimed, or zero for now.
	RunAt time.Time
	// UserID is the user the job belongs to, or 0 for none.
	UserID int64
	// SerialKey makes the job run one at a time with the jobs of the same key, or is empty for none.
	SerialKey string
}

// Job is a background job. Times are in UTC with millisecond precision.
type Job struct {
	ID      int64
	Kind    string
	Payload json.RawMessage
	Status  JobStatus
	// Attempts counts the failed attempts. A lost lease is not one.
	Attempts int64
	// Claims counts the claims of the job. Its value when a job is claimed is the fencing token of
	// that claim: a process that lost its lease holds a stale value and cannot write.
	Claims int64
	// LostLeases counts the expired leases of the job. Claim counts a lease when it finds it expired.
	LostLeases int64
	// CancelRequested is true once a cancellation of the job was requested.
	CancelRequested bool
	RunAt           time.Time
	// LockedUntil is the end of the lease, or zero when the job is not running.
	LockedUntil time.Time
	// UserID is the user the job belongs to, or 0 for none.
	UserID int64
	// SerialKey is empty for a job without a serial key.
	SerialKey string
	// LastError is the error of the last failure, or empty.
	LastError string
	CreatedAt time.Time
	// FinishedAt is the time the job ended, or zero when it did not.
	FinishedAt time.Time
}

// Cursor returns the position of the job in the lists of JobsRepository.ListByStatus, which only
// lists finished jobs.
func (j Job) Cursor() Cursor {
	return Cursor{At: j.FinishedAt, ID: j.ID}
}

// JobsRepository stores the background jobs. A job is held through the claims value that Claim
// returns: Heartbeat, Complete, Fail, Release, Retry and ConfirmCancel return ErrNotFound for a
// missing job, and ErrConflict when the job is not running under that value. Both change nothing.
type JobsRepository interface {
	// Enqueue stores a queued job with the current time as CreatedAt, and returns it.
	Enqueue(ctx context.Context, job NewJob) (Job, error)
	// Get returns the job of id, or ErrNotFound.
	Get(ctx context.Context, id int64) (Job, error)
	// Claim takes the oldest due job, by RunAt then ID, and returns it as running with a lease from
	// now. A job is due when its RunAt is reached and it is queued, or when its lease has expired,
	// which counts as a lost lease and not as a failed attempt. A job waits while another job of its
	// serial key holds a valid lease, and a queued job also waits for an expired one, so that one
	// serial key never has two running jobs.
	//
	// A job that lost its lease 5 times is marked failed instead of being claimed again, so that a
	// job that kills the process cannot loop forever. Claim returns these abandoned jobs, as they
	// are once failed, so that the caller can log their end: no later claim returns them again. A
	// job whose cancellation was requested and that no handler can confirm, because it is queued or
	// its lease has expired, is marked cancelled instead of being claimed. Claim returns ErrNotFound
	// when no job is due, along with the jobs it abandoned. The claim is atomic across processes.
	Claim(ctx context.Context, lease time.Duration) (claimed Job, abandoned []Job, err error)
	// EnqueueClaimed stores job and claims it in one atomic write, for a process that runs the job
	// itself: it returns the job running with a lease from now, and with now as its RunAt and
	// CreatedAt, whatever the RunAt of job. It returns ErrConflict and stores nothing while the serial
	// key of job is held, as Claim sees it for a queued job: by a running job, even with an expired
	// lease, or by a due queued job, which Claim would take first. A job waiting for its retry does
	// not hold its key.
	EnqueueClaimed(ctx context.Context, job NewJob, lease time.Duration) (Job, error)
	// Heartbeat renews the lease of a running job, from now, and reports whether a cancellation of
	// the job was requested.
	Heartbeat(ctx context.Context, id, claims int64, lease time.Duration) (cancelRequested bool, err error)
	// Complete marks a running job completed.
	Complete(ctx context.Context, id, claims int64) error
	// Fail marks a running job failed with message as its last error, and counts a failed attempt.
	Fail(ctx context.Context, id, claims int64, message string) error
	// Release returns a running job to queued, keeping its RunAt, its attempts and its lost leases.
	Release(ctx context.Context, id, claims int64) error
	// Retry returns a running job to queued with runAt as its RunAt and message as its last error,
	// and counts a failed attempt.
	Retry(ctx context.Context, id, claims int64, message string, runAt time.Time) error
	// Cancel cancels a queued job at once, and requests the cancellation of a running one, which its
	// handler confirms with ConfirmCancel. It returns ErrNotFound for a missing job and ErrConflict
	// for a finished one.
	Cancel(ctx context.Context, id int64) error
	// ConfirmCancel marks a running job cancelled, once its handler has stopped.
	ConfirmCancel(ctx context.Context, id, claims int64) error
	// ListSchedules returns the next run of each stored schedule of a recurring job, by name.
	ListSchedules(ctx context.Context) (map[string]time.Time, error)
	// SetSchedule creates or replaces the next run of the schedule of name.
	SetSchedule(ctx context.Context, name string, nextRun time.Time) error
	// DeleteSchedule removes the schedule of name, or returns ErrNotFound.
	DeleteSchedule(ctx context.Context, name string) error
	// HasActiveJob reports whether a job of serialKey is queued or running.
	HasActiveJob(ctx context.Context, serialKey string) (bool, error)
	// PurgeFinished removes the completed and cancelled jobs that ended before completedBefore, and
	// the failed jobs that ended before failedBefore. Queued and running jobs are never removed.
	PurgeFinished(ctx context.Context, completedBefore, failedBefore time.Time) error
	// ListByStatus returns the jobs of a finished status (see JobStatus.Finished), newest first by
	// FinishedAt then ID: at most page.Limit of them, after page.After. A finished job keeps
	// its end, so that following the position of the last job of each page lists every job once,
	// even while jobs end: they are newer than the first page. It returns ErrInvalid for another
	// status, whose jobs have no end to sort them by, and when page.Limit is below 1.
	ListByStatus(ctx context.Context, status JobStatus, page PageRequest) ([]Job, error)
	// CancelByUser cancels every queued job of the user of userID at once, and requests the
	// cancellation of their running jobs, as Cancel does. It returns the number of jobs of the user
	// still running, whose handlers have not confirmed their cancellation yet.
	CancelByUser(ctx context.Context, userID int64) (running int64, err error)
	// DeleteByUser removes at most limit jobs of the user of userID, whatever their status, and
	// returns how many it removed. It returns ErrInvalid when limit is below 1.
	DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error)
}
