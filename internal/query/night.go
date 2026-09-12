package query

import (
	"context"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// One Night read as an entity: the shape of it, and the figures that only exist
// when there is an axis to compute them on (ADR 0041).
//
// It is not a second sleep read. It runs the Metric's own rows through the
// Metric's own resolution (sleep.go), then describes what came back, so the page
// and the bar cannot disagree about the same night. Everything here is a
// property of one night; nothing here folds, which is why none of it is a
// Catalog Metric.

// NightInterval is one stage as it was recorded, in the clock's own terms.
type NightInterval struct {
	State   string `json:"state"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

// NightDetail is one Night: its intervals, the Source elected for it, and the
// figures a fold destroys.
//
// Every figure beyond Asleep is a pointer, because absent evidence is absent and
// never zero (ADR 0014): an in-bed-only night has no onset, and a night with no
// awake rows between onset and waking is not the same claim as one with zero
// minutes of them.
type NightDetail struct {
	Night      string          `json:"night"`
	Source     string          `json:"source"`
	Intervals  []NightInterval `json:"intervals"`
	Asleep     float64         `json:"asleep"`
	Awake      *float64        `json:"awake,omitempty"`
	Onset      *string         `json:"onset,omitempty"`
	Wake       *string         `json:"wake,omitempty"`
	Efficiency *float64        `json:"efficiency,omitempty"`
	// EfficiencyBasis names the denominator behind Efficiency. A percentage whose
	// denominator is assumed is a number that lies quietly, and the classic one
	// (time in bed) is not what this is: the resolution that makes this page agree
	// with the chart drops in-bed rows whenever real Stages exist (ADR 0027), so
	// the span available is the one from falling asleep to waking. The field
	// exists so the figure is never read as the other one.
	EfficiencyBasis string `json:"efficiency_basis,omitempty"`
}

// Night reads one Night by its label, the morning it woke on. ok is false when
// nothing was recorded for it, which is a different answer from an empty one and
// the caller turns it into a 404.
func (e Engine) Night(ctx context.Context, accountID int64, label string) (NightDetail, bool, error) {
	day, err := time.Parse(dayLayout, label)
	if err != nil {
		return NightDetail{}, false, ErrInvalidRange
	}

	// One day of window is one Night: sleepRows filters on the Night expression,
	// so [label, label+1) selects the intervals belonging to this night whatever
	// side of midnight they fell.
	req := Request{
		AccountID: accountID,
		Metric:    sleepKind,
		Bucket:    timeaxis.Day,
		From:      day.UTC(),
		To:        day.UTC().AddDate(0, 0, 1),
	}
	rows, err := e.sleepRows(ctx, req)
	if err != nil {
		return NightDetail{}, false, err
	}
	if len(rows) == 0 {
		return NightDetail{}, false, nil
	}

	resolved, kept, winner, ok := resolveNight(sleepKind, rows)
	if !ok {
		return NightDetail{}, false, nil
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].start.Before(kept[j].start) })

	out := NightDetail{
		Night:     label,
		Source:    winner,
		Intervals: make([]NightInterval, 0, len(kept)),
		Asleep:    resolved.value,
	}
	for _, r := range kept {
		out.Intervals = append(out.Intervals, NightInterval{
			State:   r.stage,
			StartAt: rfc3339(r.start),
			EndAt:   rfc3339(r.end),
		})
	}

	onset, wake, slept := sleepSpan(kept)
	if !slept {
		return out, true, nil // an in-bed-only night has a duration and no shape
	}
	onsetAt, wakeAt := rfc3339(onset), rfc3339(wake)
	out.Onset, out.Wake = &onsetAt, &wakeAt

	awake := awakeBetween(kept, onset, wake)
	out.Awake = &awake

	if span := wake.Sub(onset).Minutes(); span > 0 {
		efficiency := out.Asleep / span * 100
		out.Efficiency = &efficiency
		out.EfficiencyBasis = "onset_to_wake"
	}
	return out, true, nil
}

// sleepSpan is the night's asleep span: the first asleep start and the last
// asleep end. slept is false for a night with no staged sleep at all, which is
// an iPhone-only night: it has minutes and no onset, and saying so with an
// absence is more honest than naming the moment it was first in bed.
func sleepSpan(rows []sleepRow) (onset, wake time.Time, slept bool) {
	for _, r := range rows {
		if !r.isAsleep() {
			continue
		}
		if !slept || r.start.Before(onset) {
			onset = r.start
		}
		if !slept || r.end.After(wake) {
			wake = r.end
		}
		slept = true
	}
	return onset, wake, slept
}

// awakeBetween is the time awake *within* the night: interruptions, not the
// evening before or the morning after. Awake minutes outside the asleep span are
// being up, which is not a property of the night.
func awakeBetween(rows []sleepRow, onset, wake time.Time) float64 {
	var total float64
	for _, r := range rows {
		if r.stage != stageAwake {
			continue
		}
		start, end := r.start, r.end
		if start.Before(onset) {
			start = onset
		}
		if end.After(wake) {
			end = wake
		}
		if m := end.Sub(start).Minutes(); m > 0 {
			total += m
		}
	}
	return total
}
