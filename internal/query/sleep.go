package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
)

// This file is the read path for the States family: the one Metric whose values are
// intervals rather than points (ADR 0027). It sits beside the measurement engine
// rather than inside it — same Series out, different storage in — so `aggregate`,
// the Source filter and the Manual overlay stay about measurements and nothing else.

// dayLayout is the bucket-label format every Point carries.
const dayLayout = "2006-01-02"

// sleepKind is the states.kind this Metric reads. The table also holds "stand"
// hours, which no Metric reads: a stand hour is a flag, not a duration, and
// apple_stand_time already answers it in minutes as an ordinary Measurement.
const sleepKind = "sleep"

// stageInBed is the Stage that is not one: the container an iPhone records over the
// very same minutes a Watch records stages in. Counting it beside them would double
// every night, so it survives only in a Night that has no stages at all.
const stageInBed = "in_bed"

// asleepPrefix marks the Stages that count as sleep: asleep, asleep_core,
// asleep_deep, asleep_rem. `awake` and `in_bed` are reported in the breakdown and
// never summed into the Point's value.
const asleepPrefix = "asleep"

// nightExpr labels an interval with its Night: the noon-anchored day it belongs to,
// keyed on the morning it wakes into (ADR 0027).
//
// The shift is the whole rule. Apple records a night as dozens of short rows, so
// dating them by `date(start_at)` files the 23:40 rows under one day and the 02:00
// rows under the next, splitting every night in two. Shifting twelve hours forward
// first puts both halves in the same day, and that day is the waking morning — the
// one the rest of a Dashboard is talking about.
//
// Only the day form exists: week and month buckets are folded in Go from the Night
// labels themselves (see fold), so a Night can never land in a different week than
// its own label.
const nightExpr = `date(start_at, '+12 hours')`

// sleepRow is one stored interval, already labelled with its Night.
type sleepRow struct {
	night  string
	stage  string
	source string
	start  time.Time
	end    time.Time
}

// minutes is the interval's duration. A non-positive interval (an inverted or empty
// row) contributes nothing rather than a negative.
func (r sleepRow) minutes() float64 {
	d := r.end.Sub(r.start).Minutes()
	if d <= 0 {
		return 0
	}
	return d
}

// isAsleep reports whether the Stage counts as time asleep.
func (r sleepRow) isAsleep() bool { return strings.HasPrefix(r.stage, asleepPrefix) }

// seriesSleep answers a duration_by_state query: intervals in, per-Stage minutes per
// bucket out. It reads the window's rows once, resolves each Night's evidence, then
// folds the resolved Nights into the requested bucket — in that order, because the
// resolution is a rule about a Night and must not change with how the caller happens
// to be bucketing.
func (e Engine) seriesSleep(ctx context.Context, req Request, metric catalog.Metric) (Series, error) {
	out := Series{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: metric.Aggregation,
		Bucket:      req.Bucket,
		Points:      []Point{},
		Days:        windowDays(req.From, req.To),
	}

	rows, err := e.sleepRows(ctx, req)
	if err != nil {
		return Series{}, err
	}
	if len(rows) == 0 {
		return out, nil // no data in range: empty series, no Source
	}

	nights, winners := resolveNights(metric.Slug, rows)
	if len(nights) == 0 {
		return out, nil
	}
	out.Source = reportedSleepSource(metric.Slug, winners)

	out.Points = foldNights(req.Bucket, nights)
	out.Nights = len(nights)
	out.Summary = summarizeSleep(req, nights)
	return out, nil
}

// night is one resolved Night: its minutes per Stage, and the sleep figure those
// Stages come to.
type night struct {
	stages map[string]float64
	value  float64
}

// nightValue is a Night's sleep figure: time asleep, or time in bed when that is all
// that was recorded.
//
// The fallback is the other half of the in-bed rule. Dropping `in_bed` beside real
// Stages keeps a night from being counted twice; refusing to count it when it is the
// only evidence would report zero sleep to every Account that tracks nights with an
// iPhone alone — a wrong answer dressed as an empty one. `awake` is never in this
// figure at either branch.
func nightValue(stages map[string]float64) float64 {
	var asleep float64
	for stage, minutes := range stages {
		if strings.HasPrefix(stage, asleepPrefix) {
			asleep += minutes
		}
	}
	if asleep == 0 {
		return stages[stageInBed]
	}
	return asleep
}

// sleepRows reads the window's intervals, labelled by Night and ordered.
//
// The window predicate is on the *Night*, not on start_at: a night belongs to a
// window whole or not at all, which is the point of having a Night at all. Filtering
// on start_at would admit the evening half of the window's last night and cut the
// morning half off at midnight, reporting a three-hour night that was eight.
//
// The start_at bounds are there only to keep states_account_kind_start doing the
// pruning — a day of slack on each side covers every interval whose Night can fall
// inside the window — since the Night predicate is an expression the index cannot use.
func (e Engine) sleepRows(ctx context.Context, req Request) ([]sleepRow, error) {
	const q = `
		SELECT ` + nightExpr + ` AS night, state_value, source, start_at, end_at
		FROM states
		WHERE account_id = ? AND kind = ?
		  AND start_at >= ? AND start_at < ?
		  AND ` + nightExpr + ` >= ? AND ` + nightExpr + ` < ?
		ORDER BY night, start_at, id`

	from, to := req.From.UTC(), req.To.UTC()
	lo, hi := nightRange(from, to)
	rows, err := e.DB.QueryContext(ctx, q,
		req.AccountID, sleepKind,
		rfc3339(from.AddDate(0, 0, -1)), rfc3339(to.AddDate(0, 0, 1)),
		lo, hi,
	)
	if err != nil {
		return nil, fmt.Errorf("query: sleep rows: %w", err)
	}
	defer rows.Close()

	var out []sleepRow
	for rows.Next() {
		var r sleepRow
		var start, end string
		if err := rows.Scan(&r.night, &r.stage, &r.source, &start, &end); err != nil {
			return nil, fmt.Errorf("query: scan sleep row: %w", err)
		}
		// A row whose timestamps do not parse is skipped, not fatal: the Connector
		// normalizes on import, so this is a corrupt row, and one bad interval must
		// not blank a year of nights.
		var perr error
		if r.start, perr = time.Parse(time.RFC3339, start); perr != nil {
			continue
		}
		if r.end, perr = time.Parse(time.RFC3339, end); perr != nil {
			continue
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: iterate sleep rows: %w", err)
	}
	return out, nil
}

// nightRange is the half-open day range of the Nights a window covers, both bounds
// rounded *up* to a day boundary.
//
// Day-aligned windows — every window timeaxis resolves — are unaffected: [Jan 1, Jan
// 8) is the seven Nights Jan 1…Jan 7. Rounding up is what makes a now-relative window
// behave, and the Ledger builds those: with the bounds truncated instead, the last
// seven days would end at this morning's date *exclusive*, so last night — the night
// a person most wants to see — would be missing from the scoreboard until tomorrow.
// Rounding both bounds keeps the count right (seven days, seven Nights) and includes
// it.
func nightRange(from, to time.Time) (string, string) {
	return ceilDay(from).Format(dayLayout), ceilDay(to).Format(dayLayout)
}

// ceilDay is the UTC day boundary at or after t.
func ceilDay(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if t.After(day) {
		return day.AddDate(0, 0, 1)
	}
	return day
}

// resolveNights keeps, for each Night, the rows of the richest evidence available,
// and returns them with the Source that won each Night.
//
// Two mechanics in one pass, because for sleep they are the same question. Stages
// beat `in_bed`: a Night with any staged row drops its in-bed rows, a Night with none
// keeps them as its single Stage — which is an iPhone-only account's entire sleep
// history. And the Source is elected per Night rather than per window (the departure
// from ADR 0003 that ADR 0027 records): Watch and iPhone are complementary here, not
// competing, so ranking one over the whole range would silently delete every night
// the winner's device spent on a charger.
func resolveNights(slug string, rows []sleepRow) (map[string]night, map[string]string) {
	byNight := map[string][]sleepRow{}
	for _, r := range rows {
		byNight[r.night] = append(byNight[r.night], r)
	}

	kept := make(map[string]night, len(byNight))
	winners := make(map[string]string, len(byNight))
	for label, nightRows := range byNight {
		// Sources with a staged row are the richest evidence; absent any, every
		// Source present competes on its in-bed (and awake) rows alone.
		staged := map[string]bool{}
		present := map[string]bool{}
		for _, r := range nightRows {
			present[r.source] = true
			if r.isAsleep() {
				staged[r.source] = true
			}
		}
		candidates := staged
		if len(candidates) == 0 {
			candidates = present
		}
		winner, ok := catalog.ResolveSource(slug, sortedKeys(candidates))
		if !ok {
			continue
		}

		hasStages := staged[winner]
		stages := map[string]float64{}
		for _, r := range nightRows {
			if r.source != winner {
				continue
			}
			if hasStages && r.stage == stageInBed {
				continue
			}
			if m := r.minutes(); m > 0 {
				stages[r.stage] += m
			}
		}
		if len(stages) == 0 {
			continue
		}
		kept[label] = night{stages: stages, value: nightValue(stages)}
		winners[label] = winner
	}
	return kept, winners
}

// reportedSleepSource is the one Source name the Series carries when resolution ran
// per Night and several Sources may have won one. It ranks the winners by the same
// priority the per-Night election used, so the reported name is the dominant evidence
// rather than whichever night happened to be last.
func reportedSleepSource(slug string, winners map[string]string) string {
	distinct := map[string]bool{}
	for _, s := range winners {
		distinct[s] = true
	}
	source, _ := catalog.ResolveSource(slug, sortedKeys(distinct))
	return source
}

// foldNights folds the resolved Nights into the requested bucket and returns the
// ordered Points. A day bucket is one Night per Point; a week or month bucket sums
// its Nights. The bucket is computed from the Night label rather than from the
// interval, so a Night can never fall in a different week than the day it is named
// after.
//
// Values are summed per Night rather than recomputed from the merged Stages: the
// in-bed fallback is a rule about one Night's evidence, and a week holding both a
// staged night and an in-bed-only one must count each the way that night was
// recorded. In such a mixed bucket the stacked segments therefore add up to more
// than the bar's value — the only case where they can — because in-bed time and
// staged time overlap by nature.
func foldNights(bucket Bucket, nights map[string]night) []Point {
	byBucket := map[string]*Point{}
	for label, n := range nights {
		key := label
		if bucket != Day {
			day, err := time.Parse(dayLayout, label)
			if err != nil {
				continue
			}
			key = bucket.snap(day).Format(dayLayout)
		}
		p, ok := byBucket[key]
		if !ok {
			p = &Point{Bucket: key, States: map[string]float64{}}
			byBucket[key] = p
		}
		p.Value += n.value
		// A sleep bucket is folded from Nights, so its Count is nights recorded — the
		// same denominator Series.Nights carries at window scope (ADR 0027).
		p.Count++
		for stage, minutes := range n.stages {
			p.States[stage] += minutes
		}
	}

	points := make([]Point, 0, len(byBucket))
	for _, key := range sortedKeys(byBucket) {
		points = append(points, *byBucket[key])
	}
	return points
}

// summarizeSleep folds every resolved Night into one bucket spanning the range — the
// Panel summary rule, unchanged (ADR 0019). It is the sum of the Nights, not a second
// resolution at window scope, so the headline is the total of exactly the values the
// bars show. The client divides it by Series.Nights to read a per-night figure.
func summarizeSleep(req Request, nights map[string]night) *Point {
	if len(nights) == 0 {
		return nil // an empty window is a gap ("—"), never a zero
	}
	out := Point{Bucket: req.From.UTC().Format(dayLayout), States: map[string]float64{}}
	for _, n := range nights {
		out.Value += n.value
		for stage, minutes := range n.stages {
			out.States[stage] += minutes
		}
	}
	out.Count = len(nights)
	return &out
}

// sortedKeys returns a map's keys in ascending order, so every fold over a map is
// deterministic.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
