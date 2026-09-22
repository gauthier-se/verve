package now

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/day"
	"github.com/gauthier-se/verve/internal/query"
)

// The readings Now carries beside the Pins (ADR 0049). Each is one reading and not a
// window: the last value outside its Usual, the last Night, the last workout and the
// Goals in force today. None takes a range, which is what keeps Now from becoming a
// Dashboard (ADR 0025, ADR 0045).

// unusualRecentDays is how far before the Account's last datum a Metric's Latest
// value may fall and still be raised as unusual. It is measured against the Account,
// as a Source's lag is, so an export read a month late still raises its last week,
// and a Metric that stopped years ago is not raised forever for its last day.
const unusualRecentDays = 7

// maxUnusual caps the list. Past it the section stops being the few things worth a
// look and becomes a second Ledger.
const maxUnusual = 6

// Unusual reads the Metrics the Account follows, at their Latest value, and keeps
// those outside their Usual, the furthest out first. Followed means on a Panel of
// one of its Dashboards or under a Goal: the owner's own choice of what matters, so
// a copper intake nobody charts is not raised, and the read stays bounded by what
// the owner looks at rather than by everything a Source ever sent. A Pin is left
// out because its card already says where it sits. The Usual is a position and
// never a verdict (ADR 0046), so this is "outside your usual" and not "wrong".
func (e Engine) Unusual(ctx context.Context, accountID int64, now time.Time) ([]Card, error) {
	out := []Card{}
	freshness, err := e.Freshness(ctx, accountID, now)
	if err != nil {
		return nil, err
	}
	if freshness.LastDay == "" {
		return out, nil
	}
	last, err := time.Parse(dayLayout, freshness.LastDay)
	if err != nil {
		return nil, err
	}
	since := last.AddDate(0, 0, -unusualRecentDays).Format(dayLayout)

	slugs, err := e.followed(ctx, accountID)
	if err != nil {
		return nil, err
	}

	date := today(now)
	type ranked struct {
		card Card
		out  float64
	}
	var found []ranked
	for _, slug := range slugs {
		metric, ok := catalog.Lookup(slug)
		if !ok {
			continue
		}
		latest, err := e.latest(ctx, accountID, slug, date, since)
		if err != nil {
			return nil, err
		}
		if latest == nil || latest.Usual == nil {
			continue
		}
		d := outside(latest.Value, *latest.Usual)
		if d == 0 {
			continue
		}
		found = append(found, ranked{
			card: Card{Metric: metric.Slug, Unit: metric.Unit, Aggregation: metric.Aggregation, Latest: latest},
			out:  d,
		})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].out != found[j].out {
			return found[i].out > found[j].out
		}
		return found[i].card.Metric < found[j].card.Metric
	})
	for i, f := range found {
		if i == maxUnusual {
			break
		}
		out = append(out, f.card)
	}
	return out, nil
}

// followed is the Metrics on the Account's Panels or under one of its Goals, minus
// the pinned ones, once each and in slug order.
func (e Engine) followed(ctx context.Context, accountID int64) ([]string, error) {
	onPanels, err := e.Models.Panels.MetricsByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	goals, err := e.Models.Goals.ListByAccount(ctx, accountID, "")
	if err != nil {
		return nil, err
	}
	pins, err := e.Models.Pins.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	for _, p := range pins {
		skip[p.Metric] = true
	}
	var out []string
	add := func(slug string) {
		if !skip[slug] {
			skip[slug] = true
			out = append(out, slug)
		}
	}
	for _, m := range onPanels {
		add(m)
	}
	for _, g := range goals {
		add(g.Metric)
	}
	sort.Strings(out)
	return out, nil
}

// outside is how far a value sits outside its Usual, in widths of the band, so a
// step count and a heart rate rank on one scale. Zero is within. A band with no
// width (every day the same) measures against the value itself instead.
func outside(value float64, u query.Usual) float64 {
	var gap float64
	switch {
	case value > u.High:
		gap = value - u.High
	case value < u.Low:
		gap = u.Low - value
	default:
		return 0
	}
	width := u.High - u.Low
	if width <= 0 {
		width = math.Max(math.Abs(u.High), 1)
	}
	return gap / width
}

// Night is the Account's last Night, the one the sleep Metric's Latest value falls
// on, with its age. It is the Night entity itself and not a reference to a date, so
// it carries its intervals (ADR 0041): Now draws the stages as a strip that opens
// the Night's own page.
type Night struct {
	query.NightDetail
	AgeDays int `json:"age_days"`
}

// LastNight reads the last Night, or nil for an Account that never recorded one.
func (e Engine) LastNight(ctx context.Context, accountID int64, now time.Time) (*Night, error) {
	latest, err := e.Query.Latest(ctx, accountID, "sleep")
	if err != nil || latest == nil {
		return nil, err
	}
	detail, ok, err := e.Query.Night(ctx, accountID, latest.Date)
	if err != nil || !ok {
		return nil, err
	}
	return &Night{NightDetail: detail, AgeDays: daysBetween(latest.Date, today(now))}, nil
}

// Workout is the Account's last Session with its age. Its day is the one it began
// on, with none of the Night's shift, as the Day lists it (ADR 0040).
type Workout struct {
	Session  data.Session
	HasRoute bool
	AgeDays  int
}

// LastWorkout reads the Session that began last, or nil for an Account with none.
func (e Engine) LastWorkout(ctx context.Context, accountID int64, now time.Time) (*Workout, error) {
	sessions, hasRoute, err := e.Models.Sessions.ListSessions(ctx, accountID, data.SessionFilter{Limit: 1})
	if err != nil || len(sessions) == 0 {
		return nil, err
	}
	s := sessions[0]
	age := 0
	if len(s.StartAt) >= len(dayLayout) {
		age = daysBetween(s.StartAt[:len(dayLayout)], today(now))
	}
	return &Workout{Session: s, HasRoute: hasRoute[0], AgeDays: age}, nil
}

// GoalRow is one Goal in force today with the week counted against it, the way a
// Pin card carries it (ADR 0044), for every Goal and not only the pinned ones.
type GoalRow struct {
	Metric      string              `json:"metric"`
	Unit        string              `json:"unit"`
	Aggregation catalog.Aggregation `json:"aggregation"`
	Goal        day.Goal            `json:"goal"`
	// Attainment counts the last seven complete days, absent when the Goal started
	// today and has no day behind it yet.
	Attainment *query.Attainment `json:"attainment,omitempty"`
}

// GoalRows reads every Goal in force today, one per Metric, in the Account's Goal
// order. A Goal on a Metric that left the Catalog is skipped, as a Pin is.
func (e Engine) GoalRows(ctx context.Context, accountID int64, now time.Time) ([]GoalRow, error) {
	goals, err := e.Models.Goals.ListByAccount(ctx, accountID, "")
	if err != nil {
		return nil, err
	}
	date := today(now)
	day0, err := time.Parse(dayLayout, date)
	if err != nil {
		return nil, err
	}
	weekFrom := day0.AddDate(0, 0, -attainmentDays)

	out := []GoalRow{}
	seen := map[string]bool{}
	for _, g := range goals {
		if seen[g.Metric] {
			continue
		}
		bound := goalOn(goals, g.Metric, date)
		if bound == nil {
			continue
		}
		metric, ok := catalog.Lookup(g.Metric)
		if !ok {
			continue
		}
		seen[g.Metric] = true
		row := GoalRow{Metric: metric.Slug, Unit: metric.Unit, Aggregation: metric.Aggregation, Goal: *bound}
		if row.Attainment, err = e.Goals.Attain(ctx, accountID, metric.Slug, weekFrom, day0, now); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}
