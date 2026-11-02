package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// kindPurgeJobs is the recurring job that removes finished jobs.
const kindPurgeJobs = "jobs.purge"

// registerJobs adds the job kinds of the server to registry and returns its recurring jobs.
func registerJobs(registry *jobs.Registry, st store.Store, clk clock.Clock) []jobs.Schedule {
	jobs.Register(registry, kindPurgeJobs, jobs.KindOptions{}, func(ctx context.Context, _ jobs.Job[struct{}]) error {
		return jobs.Purge(ctx, st.Jobs(), clk.Now())
	})

	return []jobs.Schedule{
		{Name: kindPurgeJobs, Kind: kindPurgeJobs, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
	}
}
