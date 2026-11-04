package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// The recurring jobs that remove finished jobs, old audit events and expired sessions.
const (
	kindPurgeJobs     = "jobs.purge"
	kindPurgeAudit    = "audit.purge"
	kindPurgeSessions = "cleanup.sessions"
)

// registerJobs adds the job kinds of the server to registry and returns its recurring jobs.
func registerJobs(registry *jobs.Registry, st store.Store, clk clock.Clock, cfg config.Config) []jobs.Schedule {
	jobs.Register(registry, kindPurgeJobs, jobs.KindOptions{}, func(ctx context.Context, _ jobs.Job[struct{}]) error {
		return jobs.Purge(ctx, st.Jobs(), clk.Now())
	})

	jobs.Register(registry, kindPurgeAudit, jobs.KindOptions{}, func(ctx context.Context, _ jobs.Job[struct{}]) error {
		return audit.Purge(ctx, st.AuditEvents(), clk.Now(), cfg.Audit.RetentionDays)
	})

	jobs.Register(registry, kindPurgeSessions, jobs.KindOptions{}, func(ctx context.Context, _ jobs.Job[struct{}]) error {
		return st.Sessions().PurgeExpired(ctx, clk.Now())
	})

	return []jobs.Schedule{
		{Name: kindPurgeJobs, Kind: kindPurgeJobs, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
		{Name: kindPurgeAudit, Kind: kindPurgeAudit, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
		{Name: kindPurgeSessions, Kind: kindPurgeSessions, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
	}
}
