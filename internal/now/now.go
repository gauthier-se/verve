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
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/data"
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

// Engine reads the Now screen. It composes the read engine and the Account's rows
// and owns nothing else.
type Engine struct {
	Query  query.Engine
	Models data.Models
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
