package aggregate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/preferences"
	"github.com/gauthier-se/verve/internal/store"
)

// KindRecompute is the kind of the job that recomputes the aggregates of the dirty ranges of a user
// (FR-9 of Storage and aggregates).
const KindRecompute = "aggregate.recompute"

// errTimezoneChanged stops a recomputation whose user changed timezone since it started: the change
// queued the job that recomputes everything in the new timezone.
var errTimezoneChanged = errors.New("timezone changed")

// SerialKey returns the serial key of the recompute jobs of the user of userID, so that one
// recomputation runs at a time per user. It shares nothing with the imports, since the job reads
// committed samples only.
func SerialKey(userID int64) string {
	return "aggregate:" + strconv.FormatInt(userID, 10)
}

// recomputeJob returns the recompute job of the user of userID.
func recomputeJob(userID int64) store.NewJob {
	return store.NewJob{Kind: KindRecompute, Payload: json.RawMessage(`{}`), UserID: userID, SerialKey: SerialKey(userID)}
}

// Register adds the recompute job kind to registry, recomputing through st, and returns it.
func Register(registry *jobs.Registry, st store.Store) jobs.Kind[struct{}] {
	return jobs.Register(registry, KindRecompute, jobs.KindOptions{}, func(ctx context.Context, job jobs.Job[struct{}]) error {
		return Recompute(ctx, st, job.UserID)
	})
}

// Recompute recomputes the aggregates of the dirty ranges of userID present when it starts, in
// their timezone (FR-9 of Storage and aggregates). It merges the overlapping ranges of each type
// once widened to local hours, recomputes those hours, then the local days that hold them, one
// local month per transaction, and deletes the ranges it read once they are recomputed, through
// the largest id it read. Ranges recorded meanwhile stay, with the job that recording them queued.
//
// A recomputation stops without deleting its ranges when the user changes timezone meanwhile, and
// when the user no longer exists. Running it again is safe: it computes the same rows.
func Recompute(ctx context.Context, st store.Store, userID int64) error {
	ranges, err := st.Aggregates().ListDirty(ctx, userID)
	if err != nil {
		return fmt.Errorf("recompute aggregates of user %d: %w", userID, err)
	}

	if len(ranges) == 0 {
		return nil
	}

	prefs, err := st.Preferences().Get(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("recompute aggregates of user %d: %w", userID, err)
	}

	loc, err := preferences.LocationOf(prefs)
	if err != nil {
		return fmt.Errorf("recompute aggregates of user %d: %w", userID, err)
	}

	var lastID int64

	for start := 0; start < len(ranges); {
		end := start
		for end < len(ranges) && ranges[end].TypeID == ranges[start].TypeID {
			lastID = max(lastID, ranges[end].ID)
			end++
		}

		err := recomputeType(ctx, st, userID, loc, ranges[start:end])
		if errors.Is(err, errTimezoneChanged) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("recompute aggregates of user %d: %w", userID, err)
		}

		start = end
	}

	if err := st.Aggregates().DeleteDirtyThrough(ctx, userID, lastID); err != nil {
		return fmt.Errorf("recompute aggregates of user %d: %w", userID, err)
	}

	return nil
}

// recomputeType recomputes ranges, the dirty ranges of one type, in loc, one local month per
// transaction. It returns errTimezoneChanged when the effective timezone of the user is no longer
// loc when a transaction starts.
func recomputeType(ctx context.Context, st store.Store, userID int64, loc *time.Location, ranges []store.DirtyRange) error {
	metricType, err := st.MetricTypes().Get(ctx, ranges[0].TypeID)
	if err != nil {
		return fmt.Errorf("get type %d: %w", ranges[0].TypeID, err)
	}

	for _, w := range hourWindows(ranges, loc) {
		for from := w.from; from.Before(w.to); {
			to := monthEnd(monthStart(from, loc), loc)
			if w.to.Before(to) {
				to = w.to
			}

			err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
				prefs, err := tx.Preferences().Get(ctx, userID)
				if err != nil {
					return err
				}

				if current, err := preferences.LocationOf(prefs); err != nil || current.String() != loc.String() {
					return errTimezoneChanged
				}

				return recomputeHours(ctx, tx, userID, metricType, loc, from, to)
			})
			if err != nil {
				return fmt.Errorf("recompute %s from %s to %s: %w", metricType.Key, from.Format(time.RFC3339),
					to.Format(time.RFC3339), err)
			}

			from = to
		}
	}

	return nil
}

// window is a range of whole local hours, [from, to).
type window struct {
	from, to time.Time
}

// hourWindows returns ranges, sorted by start, widened to the local hours of loc that hold them,
// with the windows that overlap or touch merged, sorted by start. The end of a range counts: it can
// be the instant of an instantaneous sample.
func hourWindows(ranges []store.DirtyRange, loc *time.Location) []window {
	var windows []window

	for _, r := range ranges {
		w := window{from: hourStart(r.From, loc), to: hourEnd(hourStart(r.To, loc), loc)}

		if n := len(windows); n > 0 && !w.from.After(windows[n-1].to) {
			if w.to.After(windows[n-1].to) {
				windows[n-1].to = w.to
			}

			continue
		}

		windows = append(windows, w)
	}

	return windows
}

// recomputeHours replaces, through tx, the hourly rows of userID and metricType of the local hours
// of loc in [from, to), whole hours of one local month, then the daily rows of the local days that
// hold them, from the hourly rows stored.
func recomputeHours(ctx context.Context, tx store.Repositories, userID int64, metricType store.MetricType,
	loc *time.Location, from, to time.Time,
) error {
	sources, err := tx.Sources().List(ctx, userID)
	if err != nil {
		return err
	}

	stored, err := tx.SourcePriorities().Get(ctx, userID, metricType.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	priorities := ResolvePriorities(metricType, sources, stored)

	samples, err := tx.Samples().ListOverlapping(ctx, userID, metricType.ID, from, to)
	if err != nil {
		return err
	}

	hours := localHours(from, to, loc)
	byHour := samplesByHour(samples, hours, styleOf(metricType), loc)

	var rows []store.AggregateRow

	for i, hour := range hours {
		if row, ok := hourRow(metricType, priorities, hour, hourEnd(hour, loc), byHour[i]); ok {
			rows = append(rows, row)
		}
	}

	if err := tx.Aggregates().ReplaceHourly(ctx, userID, metricType.ID, from, to, rows); err != nil {
		return err
	}

	dayFrom := dayStart(from, loc)
	dayTo := dayEnd(dayStart(hours[len(hours)-1], loc), loc)

	hourly, err := tx.Aggregates().ListHourly(ctx, userID, metricType.ID, dayFrom, dayTo)
	if err != nil {
		return err
	}

	return tx.Aggregates().ReplaceDaily(ctx, userID, metricType.ID, dayFrom, dayTo, dayRows(hourly, loc))
}

// samplesByHour returns, for each hour of hours, the local hours of loc in order, the samples of
// samples, sorted by start then id, that may count in it: the hour of its start for a discrete
// sample, and every hour it overlaps otherwise, in the order of samples.
func samplesByHour(samples []store.Sample, hours []time.Time, s style, loc *time.Location) [][]store.Sample {
	byHour := make([][]store.Sample, len(hours))

	for _, sample := range samples {
		// The last hour that starts at or before the sample, or the first hour.
		first := max(sort.Search(len(hours), func(i int) bool { return hours[i].After(sample.Start) })-1, 0)

		for i := first; i < len(hours); i++ {
			if i > first && (s == discrete || !hours[i].Before(sample.End)) {
				break
			}

			if s.inBucket(sample, hours[i], hourEnd(hours[i], loc)) {
				byHour[i] = append(byHour[i], sample)
			}
		}
	}

	return byHour
}

// dayRows returns the daily rows of hourly, hourly rows sorted by start, grouped by the local day of
// loc that holds them.
func dayRows(hourly []store.AggregateRow, loc *time.Location) []store.AggregateRow {
	var days []store.AggregateRow

	for start := 0; start < len(hourly); {
		day := dayStart(hourly[start].Start, loc)

		end := start
		for end < len(hourly) && dayStart(hourly[end].Start, loc).Equal(day) {
			end++
		}

		days = append(days, dayRow(day, hourly[start:end]))
		start = end
	}

	return days
}
