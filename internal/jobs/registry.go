// Package jobs runs background work on the persistent jobs table: features register their job
// kinds in a Registry, and a Worker runs the jobs of the store.
package jobs

import (
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

// KindOptions configures a job kind.
type KindOptions struct{}

// Job is a job as its handler sees it.
type Job[P any] struct {
	ID int64
	// UserID is the user the job belongs to, or 0 for none.
	UserID  int64
	Payload P
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

// handler runs a stored job.
type handler func(ctx context.Context, job store.Job) error

// Registry holds the handlers of the job kinds. It is built once, in internal/app.
type Registry struct {
	handlers map[string]handler
}

// NewRegistry returns a registry without kinds.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]handler)}
}

// Register adds a job kind to r, with fn as its handler, and returns it. A kind registered twice
// is a programming error in the wiring, and panics.
func Register[P any](r *Registry, kind string, _ KindOptions, fn func(ctx context.Context, job Job[P]) error) Kind[P] {
	if _, ok := r.handlers[kind]; ok {
		panic(fmt.Sprintf("job kind %q registered twice", kind))
	}

	r.handlers[kind] = func(ctx context.Context, job store.Job) error {
		var payload P
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return errMalformedPayload
		}

		return fn(ctx, Job[P]{ID: job.ID, UserID: job.UserID, Payload: payload})
	}

	return Kind[P]{name: kind}
}

// run calls the handler of the kind of job.
func (r *Registry) run(ctx context.Context, job store.Job) error {
	h, ok := r.handlers[job.Kind]
	if !ok {
		return errUnknownKind
	}

	return h(ctx, job)
}
