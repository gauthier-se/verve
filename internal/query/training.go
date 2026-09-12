package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// This file is the read path for the Sessions family read as a Metric: minutes and
// kilometres of training per bucket, broken down by Activity (ADR 0040). It sits
// beside sleep.go for the same reason that file exists: same Series out, different
// storage in, so the measurement engine's Source filter, Manual overlay and
// per-bucket SQL stay about measurements and nothing else.
//
// ADR 0028 is not reopened here. A Session keeps its identity, its list and its
// page, and nothing on a chart is a workout: what a Panel carries is a fold of the
// Sessions in a bucket, which has no identity, no route and no detail view.

// The Metrics this path serves. Both read the same rows and differ only in which
// column they sum, so the pair is declared here rather than inferred from the slug
// in three places.
const (
	metricTrainingTime     = "training_time"
	metricTrainingDistance = "training_distance"
)

// breakdownSlots is how many Activities a Point's breakdown names before the rest
// are folded into `other`. Five plus `other` is six, which is the long ramp's
// length (ADR 0036) and a Night's cardinality: the stack this draws has exactly as
// many segments as the one sleep already draws.
//
// The alternative is an uncapped stack. Activity is open by construction (ADR 0011)
// and Apple ships about eighty of them, so uncapped means a legend nobody reads and
// colours repeating around the ramp.
const breakdownSlots = 5

// otherActivity is the bucket the capped-away Activities fold into. It is Verve's
// own key rather than a Source's word, which is why the client labels it rather
// than prettifying it like a slug.
const otherActivity = "other"

// trainingRow is one stored Session, reduced to what a fold needs.
type trainingRow struct {
	day      string // the calendar day the workout started, its bucket label
	activity string
	value    float64 // minutes or kilometres, per the Metric asked for
	source   string
	counted  bool // false for a Session the Metric has no figure for
}

// seriesTraining folds an Account's Sessions into the requested bucket.
func (e Engine) seriesTraining(ctx context.Context, req Request, metric catalog.Metric) (Series, error) {
	out := Series{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: metric.Aggregation,
		Bucket:      req.Bucket,
		Points:      []Point{},
		Days:        windowDays(req.From, req.To),
	}

	rows, err := e.trainingRows(ctx, req, metric.Slug)
	if err != nil {
		return Series{}, err
	}
	if len(rows) == 0 {
		return out, nil // no data in range: empty series, no Source
	}
	out.Source = reportedTrainingSource(metric.Slug, rows)

	// The cap is decided once over the whole window. Per bucket it would make a
	// segment mean "the biggest that week", which is a different quantity on every
	// bar and a legend that describes none of them.
	keep := topActivities(rows)

	out.Points = foldTraining(req.Bucket, rows, keep)
	out.Summary = summarizeTraining(req, rows, keep)
	return out, nil
}

// trainingRows reads the window's Sessions, already reduced to the Metric's figure.
//
// The bucket is the day the workout started, with no shift: a Night straddles
// midnight by nature and is dated by the morning it wakes into (ADR 0027), while a
// workout that runs through midnight is one its owner would name by the evening it
// began. The window predicate is therefore on start_at directly, which
// sessions_account_start covers.
func (e Engine) trainingRows(ctx context.Context, req Request, slug string) ([]trainingRow, error) {
	const q = `
		SELECT date(start_at) AS day, activity_type, duration, total_distance, source
		  FROM sessions
		 WHERE account_id = ? AND start_at >= ? AND start_at < ?
		 ORDER BY start_at, id`

	rows, err := e.DB.QueryContext(ctx, q, req.AccountID, rfc3339(req.From.UTC()), rfc3339(req.To.UTC()))
	if err != nil {
		return nil, fmt.Errorf("query: training rows: %w", err)
	}
	defer rows.Close()

	var out []trainingRow
	for rows.Next() {
		var (
			r        trainingRow
			duration float64
			distance *float64
		)
		if err := rows.Scan(&r.day, &r.activity, &duration, &distance, &r.source); err != nil {
			return nil, fmt.Errorf("query: scan training row: %w", err)
		}
		switch slug {
		case metricTrainingTime:
			// The column is seconds and the Metric is minutes, the unit sleep also
			// reports in, so one duration formatter serves both.
			r.value, r.counted = duration/60, true
		case metricTrainingDistance:
			// A strength session has no kilometres. It contributes nothing here and
			// still contributes its minutes to the other Metric; a zero would be a
			// claim about a workout that happened.
			if distance != nil {
				r.value, r.counted = *distance, true
			}
		}
		if r.value < 0 {
			continue // a negative figure is a corrupt row, not a workout
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: iterate training rows: %w", err)
	}
	return out, nil
}

// topActivities is the set of Activities that keep their own segment: the largest
// by total over the window, up to breakdownSlots. Ties break on the slug so the
// choice is deterministic.
//
// Activities outside the set are not dropped. Their values still count towards
// every Point's total; only their segment is folded into `other`, so the cap is a
// display choice and never changes the answer.
func topActivities(rows []trainingRow) map[string]bool {
	totals := map[string]float64{}
	for _, r := range rows {
		if r.counted {
			totals[r.activity] += r.value
		}
	}

	slugs := sortedKeys(totals)
	sort.SliceStable(slugs, func(i, j int) bool { return totals[slugs[i]] > totals[slugs[j]] })

	keep := make(map[string]bool, breakdownSlots)
	for i, slug := range slugs {
		if i >= breakdownSlots {
			break
		}
		keep[slug] = true
	}
	return keep
}

// segment is the breakdown key a row is drawn under: its own Activity, or the
// shared `other` bucket.
func segment(activity string, keep map[string]bool) string {
	if keep[activity] {
		return activity
	}
	return otherActivity
}

// foldTraining folds the rows into the requested bucket and returns the ordered
// Points. A day bucket is one calendar day; a week or month bucket sums its days,
// computed from the day label so a workout cannot land in a different week than the
// day it is named after.
//
// Point.Count is the number of Sessions behind the bucket, which is what "the
// reading count behind the figure" means for this family and what the Ledger's
// table shows in its last column. A Session with no figure for this Metric (a
// strength session under training_distance) is not counted: it is not evidence of a
// distance.
func foldTraining(bucket timeaxis.Bucket, rows []trainingRow, keep map[string]bool) []Point {
	byBucket := map[string]*Point{}
	for _, r := range rows {
		if !r.counted {
			continue
		}
		key := r.day
		if bucket != timeaxis.Day {
			day, err := time.Parse(dayLayout, r.day)
			if err != nil {
				continue
			}
			key = bucket.Start(day)
		}
		p, ok := byBucket[key]
		if !ok {
			p = &Point{Bucket: key, States: map[string]float64{}}
			byBucket[key] = p
		}
		p.Value += r.value
		p.Count++
		p.States[segment(r.activity, keep)] += r.value
	}

	points := make([]Point, 0, len(byBucket))
	for _, key := range sortedKeys(byBucket) {
		points = append(points, *byBucket[key])
	}
	return points
}

// summarizeTraining folds the window into one bucket, the Panel summary rule
// unchanged (ADR 0019): the total of exactly the values the bars show, with the
// same breakdown so a legend can name each Activity's share of the window.
func summarizeTraining(req Request, rows []trainingRow, keep map[string]bool) *Point {
	out := Point{Bucket: req.From.UTC().Format(dayLayout), States: map[string]float64{}}
	for _, r := range rows {
		if !r.counted {
			continue
		}
		out.Value += r.value
		out.Count++
		out.States[segment(r.activity, keep)] += r.value
	}
	if out.Count == 0 {
		return nil // an empty window is a gap ("—"), never a zero
	}
	return &out
}

// reportedTrainingSource names the Sources behind the figure.
//
// Nothing elects between them, and that is this milestone's one deliberate gap: no
// shipped Connector emits a workout twice, since a Takeout carries none, so there is
// nothing to resolve yet. The day a second Connector does, two Sources recording one
// run will double the volume, and the fix is the day-grain election that already
// exists for measurements (ADR 0034). Reporting what was read is what makes that
// visible rather than silent.
func reportedTrainingSource(slug string, rows []trainingRow) string {
	distinct := map[string]bool{}
	for _, r := range rows {
		if r.counted {
			distinct[r.source] = true
		}
	}
	if len(distinct) == 0 {
		return ""
	}
	return reportedSource(slug, sortedKeys(distinct))
}

// hasSessions reports whether the Account recorded any workout. It is the Sessions
// twin of hasStates, and it exists for the same reason: the Ledger's row set is a
// DISTINCT over measurements, which structurally cannot see a Metric read from
// another family.
func (e Engine) hasSessions(ctx context.Context, accountID int64) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM sessions WHERE account_id = ?)`
	var found bool
	if err := e.DB.QueryRowContext(ctx, q, accountID).Scan(&found); err != nil {
		return false, fmt.Errorf("query: has sessions: %w", err)
	}
	return found, nil
}
