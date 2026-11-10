package backup

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// KindRun is the job of a scheduled backup.
const KindRun = "backup.run"

// KindCreate is the job of a manual backup that an admin asked for.
const KindCreate = "backup.create"

// runMaxAttempts fails a backup at its first failure, so that admins see it at once (FR-9 of
// backups). The next run of the schedule tries again, and an admin can ask for a manual one.
const runMaxAttempts = 1

// CreatePayload is the payload of the backup.create job.
type CreatePayload struct {
	// ActorID is the admin who asked for the backup.
	ActorID int64 `json:"actorId"`
}

// ManualJob returns the backup.create job of a manual backup for the admin of actorID. Its serial
// key makes manual backups run one at a time.
func ManualJob(actorID int64) (store.NewJob, error) {
	payload, err := json.Marshal(CreatePayload{ActorID: actorID})
	if err != nil {
		return store.NewJob{}, fmt.Errorf("encode payload of job kind %q: %w", KindCreate, err)
	}

	return store.NewJob{Kind: KindCreate, Payload: payload, SerialKey: KindCreate}, nil
}

// Register adds the job kinds of backups to registry. The job of a scheduled backup backs up s, then
// keeps the keep newest scheduled backups. A failed backup fails the job, and retention does not
// run. The job of a manual backup backs up s for the admin who asked for it.
func Register(registry *jobs.Registry, b *Backups, s store.Store, keep int) {
	jobs.Register(registry, KindRun, jobs.KindOptions{MaxAttempts: runMaxAttempts},
		func(ctx context.Context, _ jobs.Job[struct{}]) error {
			if _, err := b.Create(ctx, s, ReasonScheduled); err != nil {
				return err
			}

			return b.ApplyRetention(ctx, keep)
		})

	jobs.Register(registry, KindCreate, jobs.KindOptions{MaxAttempts: runMaxAttempts},
		func(ctx context.Context, job jobs.Job[CreatePayload]) error {
			_, err := b.CreateManual(ctx, s, job.Payload.ActorID)

			return err
		})
}
