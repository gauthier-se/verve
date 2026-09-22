// Package now answers the Now screen: where the Account stands, rather than what a
// window looked like (CONTEXT.md: Now, Freshness, ADR 0045).
//
// It has its own module for the reason internal/day does. Freshness is read from
// the engine's families, a Pin card is a Pin, a Goal and an engine read composed,
// and none of those modules owns the others (ADR 0037).
//
// Nothing here is a window a caller picks. The age is counted against the UTC
// today, the day Goals and windows are already dated by, and the only threshold is
// the one that tells an active Source from a retired one.
package now

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/day"
	"github.com/gauthier-se/verve/internal/goal"
	"github.com/gauthier-se/verve/internal/query"
)

const dayLayout = "2006-01-02"

// retiredAfterDays is how far a Source may fall behind the Account's own last datum
// and still be active. It is measured against the Account and not against now, so a
// phone replaced years ago is retired rather than late forever, and a Watch that
// stopped three weeks before the last export is late rather than retired. A
// constant and not a setting: the ADR fixes it, as ADR 0021 fixes the Ledger's
// windows.
const retiredAfterDays = 30

// Freshness is how old the Account's data is: its last datum, and each Source's
// last datum measured against it.
type Freshness struct {
	// LastDay is the Account's last datum's day; empty for an Account with no data.
	LastDay string `json:"last_day,omitempty"`
	// AgeDays is LastDay's age against the UTC today. Zero when there is no data,
	// which LastDay being empty already says.
	AgeDays int `json:"age_days"`
	// Sources are active first, the least behind leading, then retired, the most
	// recently retired leading.
	Sources []SourceFresh `json:"sources"`
}

// SourceFresh is one Source's last datum, measured against the Account's.
type SourceFresh struct {
	Source  string `json:"source"`
	LastDay string `json:"last_day"`
	// LagDays is how far behind the Account's last datum this Source stops: zero for
	// the Source that recorded last.
	LagDays int `json:"lag_days"`
	// Retired is LagDays past the threshold. A retired Source is listed and not
	// raised: it is a device the Account stopped using, not one that went quiet.
	Retired bool `json:"retired"`
}

// attainmentDays is the week a card counts its Goal over: the seven complete days
// before today, since today is never judged (ADR 0044). A constant and not a range,
// as the Ledger's windows are (ADR 0021): Now is one reading, and a Pin still
// carries no time axis (ADR 0025).
const attainmentDays = 7

// Card is one Pin read at its Latest value, with the bound in force today and the
// week counted against it. It carries no curve and no range, which is what keeps it
// from being a one-Panel Dashboard (ADR 0025).
type Card struct {
	Metric      string              `json:"metric"`
	Unit        string              `json:"unit"`
	Aggregation catalog.Aggregation `json:"aggregation"`
	// Latest is absent for a Metric the Account pinned and never measured.
	Latest *Latest `json:"latest,omitempty"`
	// Goal is the bound in force today, printed beside the value as a fact and
	// never as a verdict: today is the one day a Goal does not judge.
	Goal *day.Goal `json:"goal,omitempty"`
	// Attainment counts the last seven complete days, absent when no Goal was in
	// force over any of them.
	Attainment *query.Attainment `json:"attainment,omitempty"`
}

// Latest is a Metric's Latest value with its age, counted like the Account's.
type Latest struct {
	Value   float64 `json:"value"`
	Date    string  `json:"date"`
	AgeDays int     `json:"age_days"`
}

// Read is the Now screen: the Account's Freshness and one card per Pin.
type Read struct {
	Freshness Freshness
	Cards     []Card
}

// Engine reads the Now screen. It composes the read engine, the Goal counts and the
// Account's rows, and owns nothing else.
type Engine struct {
	Query  query.Engine
	Goals  goal.Engine
	Models data.Models
}

// Read answers the Now screen. now fixes today for every age and count on it.
func (e Engine) Read(ctx context.Context, accountID int64, now time.Time) (Read, error) {
	freshness, err := e.Freshness(ctx, accountID, now)
	if err != nil {
		return Read{}, err
	}
	cards, err := e.Cards(ctx, accountID, now)
	if err != nil {
		return Read{}, err
	}
	return Read{Freshness: freshness, Cards: cards}, nil
}

// Cards reads each Pin at its Latest value, in Pin order. A Pin whose Metric left
// the Catalog is skipped, as the sidebar hides it (ADR 0025).
func (e Engine) Cards(ctx context.Context, accountID int64, now time.Time) ([]Card, error) {
	pins, err := e.Models.Pins.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// Every Goal of the Account in one read, then the one in force today per Pin.
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

	cards := make([]Card, 0, len(pins))
	for _, pin := range pins {
		metric, ok := catalog.Lookup(pin.Metric)
		if !ok {
			continue
		}
		card := Card{Metric: metric.Slug, Unit: metric.Unit, Aggregation: metric.Aggregation}

		latest, err := e.Query.Latest(ctx, accountID, metric.Slug)
		if err != nil && !errors.Is(err, query.ErrUnsupportedAggregation) {
			return nil, err
		}
		if latest != nil {
			card.Latest = &Latest{Value: latest.Value, Date: latest.Date, AgeDays: daysBetween(latest.Date, date)}
		}

		card.Goal = goalOn(goals, metric.Slug, date)
		if card.Attainment, err = e.Goals.Attain(ctx, accountID, metric.Slug, weekFrom, day0, now); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// goalOn is the bound on a Metric in force on a date, or nil. A Goal holds on
// [started_on, ended_on), both day labels, so string comparison is chronological.
func goalOn(goals []data.Goal, metric, date string) *day.Goal {
	for _, g := range goals {
		if g.Metric != metric || date < g.StartedOn || (g.EndedOn != nil && date >= *g.EndedOn) {
			continue
		}
		return &day.Goal{Direction: g.Direction, Value: g.Value}
	}
	return nil
}

// Freshness answers the Account's Freshness. now fixes today, so the age is pinned
// by a test rather than by the clock.
func (e Engine) Freshness(ctx context.Context, accountID int64, now time.Time) (Freshness, error) {
	days, err := e.Query.SourceLastDays(ctx, accountID)
	if err != nil {
		return Freshness{}, err
	}

	out := Freshness{Sources: make([]SourceFresh, 0, len(days))}
	for _, d := range days {
		if d.LastDay > out.LastDay {
			out.LastDay = d.LastDay
		}
	}
	if out.LastDay == "" {
		return out, nil
	}
	out.AgeDays = daysBetween(out.LastDay, today(now))

	for _, d := range days {
		lag := daysBetween(d.LastDay, out.LastDay)
		out.Sources = append(out.Sources, SourceFresh{
			Source:  d.Source,
			LastDay: d.LastDay,
			LagDays: lag,
			Retired: lag > retiredAfterDays,
		})
	}
	sort.SliceStable(out.Sources, func(i, j int) bool {
		a, b := out.Sources[i], out.Sources[j]
		if a.Retired != b.Retired {
			return !a.Retired
		}
		if a.LagDays != b.LagDays {
			return a.LagDays < b.LagDays
		}
		return a.Source < b.Source
	})
	return out, nil
}

// today is the UTC day now falls on, as a day label.
func today(now time.Time) string { return now.UTC().Format(dayLayout) }

// daysBetween counts whole days from one day label to a later one. A from after to
// is clamped to zero: a row dated in the future is a Connector's clock, not a
// negative age.
func daysBetween(from, to string) int {
	a, errA := time.Parse(dayLayout, from)
	b, errB := time.Parse(dayLayout, to)
	if errA != nil || errB != nil || !b.After(a) {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}
