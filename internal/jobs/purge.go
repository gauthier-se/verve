package jobs

import (
	"context"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// How long finished jobs are kept. Failed jobs are kept for longer, for admins to see.
const (
	finishedRetention = 7 * 24 * time.Hour
	failedRetention   = 30 * 24 * time.Hour
)

// Purge removes the finished jobs older than their retention at time now.
func Purge(ctx context.Context, jobs store.JobsRepository, now time.Time) error {
	return jobs.PurgeFinished(ctx, now.Add(-finishedRetention), now.Add(-failedRetention))
}
