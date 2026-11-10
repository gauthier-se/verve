package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
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

// deleteAccountMaxAttempts lets an account deletion wait, through its retries, for hours for the
// jobs of the user that it cancelled to stop, since the account stays disabled until it succeeds.
const deleteAccountMaxAttempts = 10

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

	jobs.Register(registry, auth.KindDeleteAccount, jobs.KindOptions{MaxAttempts: deleteAccountMaxAttempts},
		func(ctx context.Context, job jobs.Job[auth.DeleteAccountPayload]) error {
			err := auth.DeleteAccount(ctx, st, job.Payload.UserID)
			if errors.Is(err, auth.ErrUserNotDeleting) {
				return jobs.Permanent(err)
			}

			return err
		})

	return []jobs.Schedule{
		{Name: kindPurgeJobs, Kind: kindPurgeJobs, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
		{Name: kindPurgeAudit, Kind: kindPurgeAudit, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
		{Name: kindPurgeSessions, Kind: kindPurgeSessions, Every: 24 * time.Hour, Payload: json.RawMessage(`{}`)},
	}
}
