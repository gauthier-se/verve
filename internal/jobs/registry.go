// Package jobs runs background work on the persistent jobs table: features register their job
// kinds in a Registry, and a Worker runs the jobs of the store.
package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/store"
)

// Errors of jobs that no handler can run. They are fixed texts, because a job error is shown to
// admins and must never quote a payload.
var (
	errUnknownKind      = errors.New("unknown job kind")
	errMalformedPayload = errors.New("malformed job payload")
)

// defaultMaxAttempts is the number of failed attempts after which a job is marked failed.
const defaultMaxAttempts = 5

// KindOptions configures a job kind. Zero values take their defaults.
type KindOptions struct {
	// MaxAttempts is the number of failed attempts after which a job of this kind is marked failed.
	MaxAttempts int64
}

// Job is a job as its handler sees it.
type Job[P any] struct {
	ID int64
	// UserID is the user the job belongs to, or 0 for none.
	UserID  int64
	Payload P
	// LastAttempt is true when a failure of this run fails the job for good: its kind has no attempt
	// left, or the run is final, such as a run in process. A handler that keeps records of its own,
	// such as an import, ends them on such a failure.
	LastAttempt bool
}

// Kind is a registered job kind whose payloads have type P.
type Kind[P any] struct {
	name string
}

// New returns a job of this kind with payload, ready for Worker.Enqueue. The caller sets its other
// fields.
func (k Kind[P]) New(payload P) (store.NewJob, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return store.NewJob{}, fmt.Errorf("encode payload of job kind %q: %w", k.name, err)
	}

	return store.NewJob{Kind: k.name, Payload: encoded}, nil
}

// registeredKind is what a registry holds for a kind.
type registeredKind struct {
	// handler runs job. final is true for a run whose failure fails the job at once.
	handler     func(ctx context.Context, job store.Job, final bool) error
	maxAttempts int64
	// endedByClaim ends the records of job, which a claim ended without a handler, through tx. It is
	// nil for a kind without records of its own.
	endedByClaim func(ctx context.Context, tx store.Repositories, job store.Job) error
}

// Registry holds the handlers of the job kinds. It is built once, in internal/app.
type Registry struct {
	kinds map[string]registeredKind
}

// NewRegistry returns a registry without kinds.
func NewRegistry() *Registry {
	return &Registry{kinds: make(map[string]registeredKind)}
}

// Register adds a job kind to r, with fn as its handler, and returns it. A kind registered twice
// is a programming error in the wiring, and panics.
func Register[P any](r *Registry, kind string, opts KindOptions, fn func(ctx context.Context, job Job[P]) error) Kind[P] {
	if _, ok := r.kinds[kind]; ok {
		panic(fmt.Sprintf("job kind %q registered twice", kind))
	}

	maxAttempts := cmp.Or(opts.MaxAttempts, defaultMaxAttempts)

	r.kinds[kind] = registeredKind{
		maxAttempts: maxAttempts,
		handler: func(ctx context.Context, job store.Job, final bool) error {
			var payload P
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				return Permanent(errMalformedPayload)
			}

			return fn(ctx, Job[P]{
				ID:          job.ID,
				UserID:      job.UserID,
				Payload:     payload,
				LastAttempt: final || job.Attempts+1 >= maxAttempts,
			})
		},
	}

	return Kind[P]{name: kind}
}

// OnEndedByClaim sets fn as what ends the records of the jobs of kind that a claim ends without a
// handler: a job abandoned after 5 lost leases, failed, and a cancellation that no handler can
// confirm, cancelled (FR-4 and FR-9 of Job worker). Without it, such records, such as an import,
// would never end. fn receives the status the job ended with, and runs in the transaction of the
// claim, through tx, so that the records end with their job. An error of fn rolls the claim back,
// and the next claim tries again, so fn fails only when the store does. A job whose payload does
// not decode has no records to end, and fn does not run for it. A kind that r does not have is a
// programming error in the wiring, and panics.
func OnEndedByClaim[P any](r *Registry, kind Kind[P], fn func(ctx context.Context, tx store.Repositories, job Job[P], status store.JobStatus) error) {
	registered, ok := r.kinds[kind.name]
	if !ok {
		panic(fmt.Sprintf("job kind %q not registered", kind.name))
	}

	registered.endedByClaim = func(ctx context.Context, tx store.Repositories, job store.Job) error {
		var payload P
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil
		}

		return fn(ctx, tx, Job[P]{ID: job.ID, UserID: job.UserID, Payload: payload}, job.Status)
	}

	r.kinds[kind.name] = registered
}

// endedByClaim ends the records of job, which a claim ended without a handler, through tx, with the
// function that the kind of job set with OnEndedByClaim, if any.
func (r *Registry) endedByClaim(ctx context.Context, tx store.Repositories, job store.Job) error {
	kind, ok := r.kinds[job.Kind]
	if !ok || kind.endedByClaim == nil {
		return nil
	}

	return kind.endedByClaim(ctx, tx, job)
}

// run calls the handler of the kind of job, for a final run when final is true.
func (r *Registry) run(ctx context.Context, job store.Job, final bool) error {
	kind, ok := r.kinds[job.Kind]
	if !ok {
		return Permanent(errUnknownKind)
	}

	return kind.handler(ctx, job, final)
}

// maxAttempts returns the number of failed attempts after which a job of kind is marked failed.
func (r *Registry) maxAttempts(kind string) int64 {
	return r.kinds[kind].maxAttempts
}
