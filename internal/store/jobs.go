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
	// job that kills the process cannot loop forever. A job whose cancellation was requested and that
	// no handler can confirm, because it is queued or its lease has expired, is marked cancelled
	// instead of being claimed. Claim returns ErrNotFound when no job is due. The claim is atomic
	// across processes.
	Claim(ctx context.Context, lease time.Duration) (Job, error)
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
}
