// Package goal counts a Goal's Attainment over a window (CONTEXT.md: Goal,
// Attainment, ADR 0044).
//
// It has its own module for the reason internal/day does. The values judged are the
// read engine's day buckets, and the bounds are rows of the goals table, which the
// read engine aggregates nothing from and has no business naming (ADR 0037).
//
// Nothing here folds a day. Every value judged is a Point of Engine.Series at day
// grain, so the Source election (ADR 0034), the Manual overlay, Exclusions (ADR
// 0033), a derived Metric's Formula and sleep's Night all apply as they do to the
// bar drawn above the same date, and a Goal is judged against exactly that number.
package goal

import (
	"context"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

const dayLayout = "2006-01-02"

// chunkDays bounds each day-grain read, well under the engine's point cap: an `all`
// range spans years, and the count must cover all of it rather than lift the cap.
const chunkDays = 366

// Engine counts Attainment. It composes the read engine and the goals table and
// owns nothing else.
type Engine struct {
	Query  query.Engine
	Models data.Models
}

// Attain returns the Attainment of a Metric's Goals over [from, to), or nil when no
// Goal is in force at any point of it. now fixes today, which is never judged:
// windows the time axis resolves from a preset already end on it, and a custom one
// that runs past it is judged only up to it.
func (e Engine) Attain(ctx context.Context, accountID int64, metric string, from, to, now time.Time) (*query.Attainment, error) {
	goals, err := e.Models.Goals.InForce(ctx, accountID, metric, from.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return nil, nil
	}

	today := now.UTC().Truncate(24 * time.Hour)
	judgedTo := to
	if today.Before(judgedTo) {
		judgedTo = today
	}

	out := &query.Attainment{Segments: make([]query.GoalSegment, 0, len(goals))}
	for _, g := range goals {
		start, end, err := span(g, from, to)
		if err != nil {
			return nil, err
		}
		out.Segments = append(out.Segments, query.GoalSegment{
			Direction: g.Direction, Value: g.Value,
			From: start.Format(dayLayout), To: end.Format(dayLayout),
		})

		if judgedTo.Before(end) {
			end = judgedTo
		}
		if !start.Before(end) {
			continue // wholly today or later: in force, and not yet judged
		}
		if err := e.count(ctx, accountID, metric, g, start, end, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// span is a Goal's days clipped to [from, to).
func span(g data.Goal, from, to time.Time) (time.Time, time.Time, error) {
	start, err := time.Parse(dayLayout, g.StartedOn)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end := to
	if g.EndedOn != nil {
		e, err := time.Parse(dayLayout, *g.EndedOn)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if e.Before(end) {
			end = e
		}
	}
	if start.Before(from) {
		start = from
	}
	return start, end, nil
}

// count adds one Goal's days on [start, end) to out, reading the day buckets in
// chunks. A day the engine returns no Point for is a gap: covered, not measured.
func (e Engine) count(ctx context.Context, accountID int64, metric string, g data.Goal, start, end time.Time, out *query.Attainment) error {
	for lo := start; lo.Before(end); lo = lo.AddDate(0, 0, chunkDays) {
		hi := lo.AddDate(0, 0, chunkDays)
		if end.Before(hi) {
			hi = end
		}
		series, err := e.Query.Series(ctx, query.Request{
			AccountID: accountID, Metric: metric, Bucket: timeaxis.Day, From: lo, To: hi,
		})
		if err != nil {
			return err
		}
		out.Covered += len(timeaxis.Day.Starts(lo, hi))
		for _, p := range series.Points {
			if p.Gap {
				continue
			}
			out.Measured++
			if within(g, p.Value) {
				out.Met++
			}
		}
	}
	return nil
}

// within reports whether a day's value meets the Goal. Both directions are
// inclusive: 7 500 steps meets "at least 7 500".
func within(g data.Goal, v float64) bool {
	if g.Direction == data.GoalAtMost {
		return v <= g.Value
	}
	return v >= g.Value
}
