// Package day answers the Day page: one calendar date read as an index of
// everything the Account holds on it (CONTEXT.md: Day, ADR 0043).
//
// It has its own module for the reason internal/history does. A Day's figures are
// aggregated Measurements and belong to internal/query; its Night is that module's
// entity read; and the rest is Sessions, Annotations, Manual rows, Exclusions and
// Phases, five tables the read engine never aggregates and has no business naming.
// Composing them is neither module's job, and an HTTP handler is where that kind
// of composition goes to rot (ADR 0037).
//
// Nothing here is a new grain. Every figure is the day bucket /v1/series already
// serves, asked for one day, which is what keeps this page from becoming the
// sub-day Series contract ADR 0012 refuses: there is no parameter to widen.
package day

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// dayLayout is how a Day is addressed: the date, and nothing finer.
const dayLayout = "2006-01-02"

// maxWorkouts caps the workouts one Day lists. A day past it is not a day, and the
// cap exists so a pathological import cannot make one page unbounded.
const maxWorkouts = 200

// ErrInvalidDate is a date that is not a YYYY-MM-DD day. The HTTP layer turns it
// into a validation error rather than a 404: the request is malformed, not missing.
var ErrInvalidDate = errors.New("day: invalid date")

// Metric is one Metric's figure for the date, folded by that Metric's own rule so
// it is the same number the bar above the same date draws.
type Metric struct {
	Metric      string              `json:"metric"`
	Unit        string              `json:"unit"`
	Aggregation catalog.Aggregation `json:"aggregation"`
	// Value is nil for a gap and never 0: a day with no steps recorded is not a day
	// with no steps (ADR 0014).
	Value *float64 `json:"value,omitempty"`
	// Source is the Source elected for this Metric on this date. The election
	// already runs per day (ADR 0034), so this field displays a decision rather
	// than making one, and the Day is the only screen at the grain it is decided at.
	Source string `json:"source,omitempty"`
	// Pinned marks a row that is here because the Account pinned the Metric, present
	// or absent. It orders the list and carries no time axis of its own: the Day
	// supplies the date (ADR 0025).
	Pinned bool `json:"pinned,omitempty"`
	// Excluded says the absence is a refusal and not a gap: an Exclusion covers this
	// Metric on this date (ADR 0033). No other screen can tell the two apart.
	Excluded bool `json:"excluded,omitempty"`
	// Goal is the bound in force on this date, if the Account set one (ADR 0044). It
	// is carried as a fact and never as a verdict: the page prints the bound beside
	// the value and says nothing about which side of it the day fell on.
	Goal *Goal `json:"goal,omitempty"`
}

// Goal is a Goal's bound as the Day shows it: the direction and the value, in the
// Metric's canonical unit. Its dates are the Goal's business, not the Day's.
type Goal struct {
	Direction string  `json:"direction"`
	Value     float64 `json:"value"`
}

// SessionRef is one Session on the Day with whether it carries a Route: the pair
// the workouts list already shows together. The domain word is Session even though
// the interface says Workouts, because the API follows the domain (CONTEXT.md).
type SessionRef struct {
	Session  data.Session
	HasRoute bool
}

// Read is one Day. It carries domain rows rather than wire types: the HTTP layer
// maps them onto the views the workouts, notes, manual entries and exclusions
// already have, so a workout on a Day is byte-identical to the same workout in the
// list and nothing here is a second definition of one.
type Read struct {
	Date    string
	Metrics []Metric
	// Night is the Night labelled by this date: the one that woke into this morning
	// (ADR 0027), which is the night the sleep Panel already draws on it. The shape
	// of it is a link and not a payload, so only the figures travel (ADR 0041).
	Night       *query.NightSummary
	Sessions    []SessionRef
	Annotations []data.Annotation
	Manual      []data.Measurement
	Exclusions  []data.Exclusion
	Phase       *data.Phase
}

// Engine reads the Day page. It composes the two modules the page needs and owns
// nothing else.
type Engine struct {
	Query  query.Engine
	Models data.Models
}

// Read answers one Day, addressed by its date.
//
// A date with nothing on it is an empty Read and never an error: a date exists
// whether or not anything happened on it, which is the sharpest difference between
// a Day and every other addressable thing in Verve.
func (e Engine) Read(ctx context.Context, accountID int64, date string) (Read, error) {
	day, err := time.Parse(dayLayout, date)
	if err != nil {
		return Read{}, ErrInvalidDate
	}
	next := day.AddDate(0, 0, 1).Format(dayLayout)

	out := Read{
		Date:        date,
		Metrics:     []Metric{},
		Sessions:    []SessionRef{},
		Annotations: []data.Annotation{},
		Manual:      []data.Measurement{},
		Exclusions:  []data.Exclusion{},
	}

	// The Manual rows are read first because the figures depend on them: a typed
	// value displaces the day it falls on (ADR 0022), and the day is exactly that
	// overlay's grain, so a Metric with a Manual row today is sourced from it.
	if out.Manual, err = e.Models.Measurements.ListManualOn(ctx, accountID, date); err != nil {
		return Read{}, err
	}
	if out.Metrics, out.Exclusions, err = e.metrics(ctx, accountID, day, date, next, manualMetrics(out.Manual)); err != nil {
		return Read{}, err
	}
	if out.Night, err = e.night(ctx, accountID, date); err != nil {
		return Read{}, err
	}
	if out.Sessions, err = e.sessions(ctx, accountID, date, next); err != nil {
		return Read{}, err
	}
	if out.Annotations, err = e.Models.Annotations.ListByWindow(ctx, accountID, date, next); err != nil {
		return Read{}, err
	}
	if out.Phase, err = e.phase(ctx, accountID, date); err != nil {
		return Read{}, err
	}
	return out, nil
}

// metrics builds the figure rows and, in the same pass, the Exclusions covering the
// date. The two are read together because they answer one question between them:
// an absent value is a gap unless a rule refused it.
func (e Engine) metrics(ctx context.Context, accountID int64, day time.Time, date, next string, manual map[string]bool) ([]Metric, []data.Exclusion, error) {
	pins, err := e.Models.Pins.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	rules, err := e.Models.Exclusions.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	slugs, err := e.Query.MetricsWithDataIn(ctx, accountID, date, next)
	if err != nil {
		return nil, nil, err
	}
	// Every Goal of the Account in one read, then the one in force on this date per
	// Metric: a Goal changes nothing about which rows appear, only what they carry.
	goals, err := e.Models.Goals.ListByAccount(ctx, accountID, "")
	if err != nil {
		return nil, nil, err
	}

	covering := []data.Exclusion{}
	excluded := map[string]bool{}
	for _, rule := range rules {
		if !covers(rule, date) {
			continue
		}
		covering = append(covering, rule)
		excluded[rule.Metric] = true
	}

	// Pins first, in sidebar order, then everything else in Catalog order. A pinned
	// Metric with no data that day is the most informative row on the page: it says
	// the day is missing, not that the Metric is.
	//
	// A refused Metric has no data by construction, so without the rule it would have
	// no row and the refusal would be invisible on the one screen that can name it
	// (ADR 0033). The rule is what puts the row there.
	pinned := map[string]bool{}
	for _, p := range pins {
		pinned[p.Metric] = true
	}
	rest := []string{}
	seen := map[string]bool{}
	for _, slug := range append(slugs, keys(excluded)...) {
		if pinned[slug] || seen[slug] {
			continue
		}
		seen[slug] = true
		rest = append(rest, slug)
	}
	sort.Strings(rest)

	ordered := make([]string, 0, len(pins)+len(rest))
	for _, p := range pins {
		ordered = append(ordered, p.Metric)
	}
	ordered = append(ordered, rest...)

	rows := make([]Metric, 0, len(ordered))
	for _, slug := range ordered {
		metric, ok := catalog.Lookup(slug)
		if !ok {
			continue // a Pin whose Metric left the Catalog, or a slug with no rule to fold by
		}
		row, ok, err := e.figure(ctx, accountID, day, metric)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue // an aggregation the engine does not serve, skipped rather than failing the page
		}
		row.Pinned = pinned[slug]
		row.Excluded = excluded[slug]
		row.Goal = goalOn(goals, slug, date)
		// A Manual row on this date displaces the imported one for it (ADR 0022), so
		// the Day names what its own figure came from. The Series reports the imported
		// Source over a window deliberately, because the overlay is one day of many;
		// here the window *is* that day, and the same answer would be wrong.
		if manual[slug] {
			row.Source = catalog.SourceManual
		}
		rows = append(rows, row)
	}
	return rows, covering, nil
}

// goalOn is the Goal on a Metric in force on a date, or nil. A Goal holds on
// [started_on, ended_on), both YYYY-MM-DD, so string comparison is chronological, and
// segments never overlap, so at most one matches.
func goalOn(goals []data.Goal, metric, date string) *Goal {
	for _, g := range goals {
		if g.Metric != metric || date < g.StartedOn || (g.EndedOn != nil && date >= *g.EndedOn) {
			continue
		}
		return &Goal{Direction: g.Direction, Value: g.Value}
	}
	return nil
}

// keys is the sorted-later, unordered key set of a membership map.
func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// manualMetrics is the set of Metrics the Account typed a value for on this date.
func manualMetrics(rows []data.Measurement) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.Metric] = true
	}
	return out
}

// figure reads one Metric's day bucket through the engine's own path: the same
// rule, the same Manual overlay, the same per-day Source election. ok is false for
// an aggregation the engine cannot serve, which is a row to skip and not an error.
func (e Engine) figure(ctx context.Context, accountID int64, day time.Time, metric catalog.Metric) (Metric, bool, error) {
	series, err := e.Query.Series(ctx, query.Request{
		AccountID: accountID,
		Metric:    metric.Slug,
		Bucket:    timeaxis.Day,
		From:      day.UTC(),
		To:        day.UTC().AddDate(0, 0, 1),
	})
	if err != nil {
		if errors.Is(err, query.ErrUnsupportedAggregation) || errors.Is(err, query.ErrUnknownMetric) {
			return Metric{}, false, nil
		}
		return Metric{}, false, err
	}

	row := Metric{
		Metric:      metric.Slug,
		Unit:        series.Unit,
		Aggregation: series.Aggregation,
		Source:      series.Source,
	}
	// One day at day grain is at most one bucket. No point is a gap, which is the
	// absent value rather than a zero.
	if len(series.Points) > 0 {
		value := series.Points[0].Value
		row.Value = &value
	}
	return row, true, nil
}

// night reads the Night labelled by this date and keeps only its figures. It goes
// through the entity read rather than re-deriving anything, so the Day, the Night
// page and the sleep bar cannot disagree about the same night.
func (e Engine) night(ctx context.Context, accountID int64, date string) (*query.NightSummary, error) {
	detail, ok, err := e.Query.Night(ctx, accountID, date)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	summary := detail.NightSummary
	return &summary, nil
}

// sessions lists the Sessions that started on the date, with none of the Night's
// noon shift: a ride that runs past midnight is one its owner names by the evening
// it began (ADR 0040).
//
// The model lists newest first, which is what a browse wants; a Day is a
// chronology, so the page reads it forwards.
func (e Engine) sessions(ctx context.Context, accountID int64, date, next string) ([]SessionRef, error) {
	sessions, hasRoute, err := e.Models.Sessions.ListSessions(ctx, accountID, data.SessionFilter{
		From:  date,
		To:    next,
		Limit: maxWorkouts,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SessionRef, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		out = append(out, SessionRef{Session: sessions[i], HasRoute: hasRoute[i]})
	}
	return out, nil
}

// phase is the Phase the date falls in, open or closed: the one piece of standing
// context that explains a day rather than describing it.
func (e Engine) phase(ctx context.Context, accountID int64, date string) (*data.Phase, error) {
	phases, err := e.Models.Phases.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for i, p := range phases {
		if dayOf(p.StartedAt) > date {
			continue
		}
		// An open Phase has no end and runs to today; a closed one ends on the day it
		// was closed, and that day still belongs to it.
		if p.EndedAt != nil && dayOf(*p.EndedAt) < date {
			continue
		}
		return &phases[i], nil
	}
	return nil, nil
}

// covers reports whether one Exclusion refuses its Metric on a day. It asks the
// rule rather than restating it: ExclusionSet.Excludes is the Go twin of the SQL
// purge and the two are pinned to each other by test (internal/data), so a second
// implementation here would be a third answer waiting to disagree.
func covers(rule data.Exclusion, date string) bool {
	return data.ExclusionSet{rule.Metric: {rule}}.Excludes(rule.Metric, date)
}

// dayOf reduces a stored instant to the day it fell on. A Phase bound reaches this
// module in either shape, RFC 3339 for one Verve timestamped and YYYY-MM-DD for one
// an Account dated, and a Day compares days.
func dayOf(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(dayLayout)
	}
	return s
}
