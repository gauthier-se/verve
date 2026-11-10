package backup

import (
	"context"

	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// KindRun is the job of a scheduled backup.
const KindRun = "backup.run"

// runMaxAttempts fails a scheduled backup at its first failure, so that admins see it at once
// (FR-9 of backups). The next run of the schedule tries again.
const runMaxAttempts = 1

// Register adds the job kind of scheduled backups to registry. Its job backs up s, then keeps the
// keep newest scheduled backups. A failed backup fails the job, and retention does not run.
func Register(registry *jobs.Registry, b *Backups, s store.Store, keep int) {
	jobs.Register(registry, KindRun, jobs.KindOptions{MaxAttempts: runMaxAttempts},
		func(ctx context.Context, _ jobs.Job[struct{}]) error {
			if _, err := b.Create(ctx, s, ReasonScheduled); err != nil {
				return err
			}

			return b.ApplyRetention(keep)
		})
}
