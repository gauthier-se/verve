package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// scheduleKeyPrefix starts the serial key of the jobs of a schedule.
const scheduleKeyPrefix = "schedule:"

// Scheduler enqueues the recurring jobs of a worker. It runs inside Worker.Run, which only the
// server calls, so that a CLI process schedules nothing.
type Scheduler struct {
	worker    *Worker
	schedules []Schedule
	location  *time.Location
}

// loop ticks at once, so that a run missed during downtime happens after startup, and then at every
// poll until ctx is cancelled.
func (s *Scheduler) loop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.worker.logger.WarnContext(ctx, "schedule recurring jobs", slog.Any("error", err))
		}

		select {
		case <-time.After(s.worker.opts.PollInterval):
		case <-ctx.Done():
		}
	}
}

// Tick brings every schedule up to date with the clock, and enqueues the jobs that are due. A
// schedule that fails does not stop the others: Tick returns their errors joined. It only writes
// when something changed, so that the write lock of another process does not hold it up otherwise.
func (s *Scheduler) Tick(ctx context.Context) error {
	stored, err := s.worker.store.Jobs().ListSchedules(ctx)
	if err != nil {
		return err
	}

	now := s.worker.clk.Now().In(s.location)

	var errs []error

	for _, schedule := range s.schedules {
		nextRun, isStored := stored[schedule.Name]

		if err := s.update(ctx, schedule, nextRun, isStored, now); err != nil {
			errs = append(errs, fmt.Errorf("schedule %q: %w", schedule.Name, err))
		}
	}

	return errors.Join(errs...)
}

// update handles schedule at time now, with its stored next run if it has one. A schedule that is
// not stored yet has missed nothing: it only gets its first run.
func (s *Scheduler) update(ctx context.Context, schedule Schedule, nextRun time.Time, isStored bool, now time.Time) error {
	jobs := s.worker.store.Jobs()

	switch {
	case !schedule.enabled() && isStored:
		return jobs.DeleteSchedule(ctx, schedule.Name)
	case !schedule.enabled(), isStored && now.Before(nextRun):
		return nil
	}

	next, err := schedule.Next(now)
	if err != nil {
		return err
	}

	if !isStored {
		return jobs.SetSchedule(ctx, schedule.Name, next)
	}

	return s.enqueue(ctx, schedule, next)
}

// enqueue enqueues the job of a due schedule and stores its next run in one transaction, so that a
// crash can neither repeat the run nor lose it. However many runs were missed, this is one job. A
// job of the schedule still queued or running takes the place of this run, which is skipped.
func (s *Scheduler) enqueue(ctx context.Context, schedule Schedule, next time.Time) error {
	serialKey := scheduleKeyPrefix + schedule.Name

	var skipped bool

	err := s.worker.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		var err error

		skipped, err = tx.Jobs().HasActiveJob(ctx, serialKey)
		if err != nil {
			return err
		}

		if !skipped {
			job := store.NewJob{Kind: schedule.Kind, Payload: schedule.Payload, SerialKey: serialKey}
			if _, err := s.worker.Enqueue(ctx, tx, job); err != nil {
				return err
			}
		}

		return tx.Jobs().SetSchedule(ctx, schedule.Name, next)
	})
	if err != nil {
		return err
	}

	if skipped {
		s.worker.logger.InfoContext(ctx, "recurring job skipped", slog.String("name", schedule.Name))
	}

	return nil
}
