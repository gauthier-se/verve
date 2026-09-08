// Package history answers the History page: the long view of everything an
// Account holds, drawn over its own real extent, with the dated events that
// explain the shape (CONTEXT.md: History).
//
// It has its own module because it is the one read that genuinely spans families.
// The band is aggregated Measurements or States and belongs to internal/query; the
// events are Imports, Phases, Exclusions and Annotations, four tables the read
// engine never aggregates and has no business naming. Composing the two is neither
// module's job, and doing it in an HTTP handler is how the grid materialisation,
// the phase folding and the bucket thresholds ended up there in the first place.
package history

import (
	"context"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/connector/registry"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

const dayLayout = "2006-01-02"

// defaultMetric is the Metric the long band draws when none is asked for. Body
// mass is the one Metric almost every Account has across its whole history and the
// one a Phase is actually about, which is what makes the bands legible.
const DefaultMetric = "body_mass"

// Event kinds, in the order they are worth reading when two land on the same day:
// what arrived, then what the Account decided, then what it wrote, then what the
// data itself did.
const (
	KindImport    = "import"
	KindPhase     = "phase"
	KindExclusion = "exclusion"
	KindNote      = "note"
	KindSource    = "source"
	KindOrigin    = "origin"
)

// Phase is one Phase folded onto the band's grid: its span in buckets, its signed
// rate, and the kind that rate makes it.
type Phase struct {
	ID             int64   `json:"id"`
	Kind           string  `json:"kind"`
	RatePctPerWeek float64 `json:"rate_pct_per_week"`
	StartedOn      string  `json:"started_on"`
	EndedOn        *string `json:"ended_on,omitempty"`
	From           string  `json:"from"`
	To             string  `json:"to"`
}

// Figure is one key/number chip under an Event. The key is a stable slug the
// client labels; the value is already in its unit.
type Figure struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// Event is one dated entry in the ledger of things that happened. This module says
// what happened and when, with the figures behind it; the words belong to the
// interface, which is why it carries a kind and typed fields rather than a
// sentence.
type Event struct {
	Kind    string   `json:"kind"`
	Date    string   `json:"date"`
	EndsOn  *string  `json:"ends_on,omitempty"`
	Label   string   `json:"label,omitempty"`
	Body    string   `json:"body,omitempty"`
	Figures []Figure `json:"figures,omitempty"`
	Rate    *float64 `json:"rate_pct_per_week,omitempty"`
	// SpanFrom/SpanTo are the days an event is *about*, which for an Exclusion is
	// not the day it happened: "excluded body fat from 2026" is a decision taken on
	// one date about an open-ended stretch of another. They are deliberately not
	// EndsOn, which draws a band across the axis from Date.
	SpanFrom string `json:"span_from,omitempty"`
	SpanTo   string `json:"span_to,omitempty"`
}

// Band is the long view of one Metric with the Account's Phases folded onto the
// same grid.
type Band struct {
	query.Band
	Phases []Phase `json:"phases"`
}

// Read is the whole History page: how far back the data goes, the long band, and
// the dated events that explain its shape.
type Read struct {
	First  string  `json:"first,omitempty"`
	Last   string  `json:"last,omitempty"`
	Days   int     `json:"days"`
	Band   *Band   `json:"band,omitempty"`
	Events []Event `json:"events"`
}

// Engine reads the History page. It composes the two modules the page needs and
// owns nothing of its own beyond that composition.
type Engine struct {
	Query  query.Engine
	Models data.Models
}

// Read answers the page for one Metric.
//
// It is one call rather than five because the page is one reading. The band's
// grain, the Phase spans folded onto it, the gaps, and the events all have to
// agree about the same axis — and an interface that assembled them from four
// endpoints would be deriving that agreement client-side, which is the thing the
// read path does not do.
func (e Engine) Read(ctx context.Context, accountID int64, metric string) (Read, error) {
	span, err := e.Models.Span(ctx, accountID)
	if err != nil {
		return Read{}, err
	}

	out := Read{Events: []Event{}}

	// An Account with no data still gets a page: the events (there are none) and no
	// band. Nothing here invents a window out of a preset when the real extent is
	// knowable.
	if span.First != "" {
		first, err := time.Parse(time.RFC3339, span.First)
		if err != nil {
			return Read{}, err
		}
		last, err := time.Parse(time.RFC3339, span.Last)
		if err != nil {
			return Read{}, err
		}
		out.First = first.UTC().Format(dayLayout)
		out.Last = last.UTC().Format(dayLayout)
		out.Days = int(last.Sub(first).Hours()/24) + 1

		band, err := e.band(ctx, accountID, metric, first, last)
		if err != nil {
			return Read{}, err
		}
		out.Band = band
	}

	events, err := e.events(ctx, accountID, span)
	if err != nil {
		return Read{}, err
	}
	out.Events = events
	return out, nil
}

// band reads the dense series and folds the Account's Phases onto its grid.
func (e Engine) band(ctx context.Context, accountID int64, metric string, first, last time.Time) (*Band, error) {
	b, err := e.Query.Band(ctx, query.BandRequest{
		AccountID: accountID, Metric: metric, First: first, Last: last,
	})
	if err != nil {
		return nil, err
	}

	phases, err := e.Models.Phases.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	bucket := timeaxis.Bucket(b.Bucket)
	return &Band{Band: b, Phases: foldPhases(phases, bucket, b.Grid, last)}, nil
}

// foldPhases places each Phase on the band's grid, dropping the ones that fall
// entirely outside it and clamping the ones that overlap it at either end. An open
// Phase runs to the end of the history.
func foldPhases(phases []data.Phase, bucket timeaxis.Bucket, grid []string, last time.Time) []Phase {
	out := []Phase{}
	if len(grid) == 0 {
		return out
	}
	firstKey, lastKey := grid[0], grid[len(grid)-1]

	for _, p := range phases {
		started, err := time.Parse(dayLayout, p.StartedAt)
		if err != nil {
			continue
		}
		ended := last.UTC()
		if p.EndedAt != nil {
			if t, err := time.Parse(dayLayout, *p.EndedAt); err == nil {
				ended = t
			}
		}
		if ended.Before(started) {
			continue
		}
		from := bucket.Start(started)
		to := bucket.Start(ended)
		if to < firstKey || from > lastKey {
			continue // outside the drawn history entirely
		}
		if from < firstKey {
			from = firstKey
		}
		if to > lastKey {
			to = lastKey
		}
		out = append(out, Phase{
			ID: p.ID, Kind: p.Kind(), RatePctPerWeek: p.RatePctPerWeek,
			StartedOn: p.StartedAt, EndedOn: p.EndedAt, From: from, To: to,
		})
	}
	return out
}

// events gathers every dated event into one list, most recent first.
func (e Engine) events(ctx context.Context, accountID int64, span data.Span) ([]Event, error) {
	events := []Event{}

	imports, err := e.Models.Imports.List(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, imp := range imports {
		events = append(events, Event{
			// The Connector goes in the Label rather than in a field of its own: the
			// History renders every event through one shape, and a key only one kind
			// ever sets would be a schema change to say a sentence. With two
			// Connectors, "export.zip" alone no longer says where a history came from,
			// which is the one thing this page exists to explain.
			Kind: KindImport, Date: dayOf(imp.ImportedAt), Label: importLabel(imp.Connector, imp.SourceFile),
			Figures: []Figure{
				{Key: "added", Value: float64(imp.AddedCount)},
				{Key: "skipped", Value: float64(imp.SkippedCount)},
				{Key: "unmapped", Value: float64(imp.UnmappedCount)},
			},
		})
	}

	phases, err := e.Models.Phases.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, p := range phases {
		rate := p.RatePctPerWeek
		events = append(events, Event{
			Kind: KindPhase, Date: p.StartedAt, EndsOn: p.EndedAt,
			Label: p.Kind(), Rate: &rate,
		})
	}

	// An Exclusion is a dated fact about the shape of the data: a stretch of a curve
	// is missing because it was refused, not because nothing was recorded.
	exclusions, err := e.Models.Exclusions.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, x := range exclusions {
		events = append(events, Event{
			Kind: KindExclusion, Date: dayOf(x.CreatedAt), Label: x.Metric,
			SpanFrom: x.StartsOn, SpanTo: x.EndsOn,
			Figures: []Figure{{Key: "purged", Value: float64(x.Purged)}},
		})
	}

	notes, err := e.Models.Annotations.ListAll(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		events = append(events, Event{
			Kind: KindNote, Date: n.StartsOn, EndsOn: n.EndsOn,
			Label: n.Label, Body: valueOrEmpty(n.Body),
		})
	}

	sources, err := e.Query.Sources(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, src := range sources {
		events = append(events, Event{
			Kind: KindSource, Date: dayOf(src.FirstSeen), Label: src.Source,
			Figures: []Figure{{Key: "rows", Value: float64(src.Rows)}},
		})
	}

	// The origin: the oldest thing the Account holds. It closes the list because it
	// is where the history starts, and it is stated rather than implied — the first
	// bucket of a chart is not the same claim as "this is the earliest record you
	// have, and nothing before it was dropped".
	if span.First != "" {
		unmapped, err := e.Models.Imports.CountUnmapped(ctx, accountID)
		if err != nil {
			return nil, err
		}
		event := Event{Kind: KindOrigin, Date: dayOf(span.First)}
		if unmapped > 0 {
			event.Figures = []Figure{{Key: "unmapped_kept", Value: float64(unmapped)}}
		}
		events = append(events, event)
	}

	// Newest first, and on a tie the kind order above, so a day carrying an import
	// and the Phase it revealed reads in that order rather than at random.
	kindRank := map[string]int{
		KindImport: 0, KindPhase: 1, KindExclusion: 2, KindNote: 3, KindSource: 4, KindOrigin: 5,
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Date != events[j].Date {
			return events[i].Date > events[j].Date
		}
		return kindRank[events[i].Kind] < kindRank[events[j].Kind]
	})
	return events, nil
}

// dayOf reduces a stored instant to the day it fell on. Rows reach this module in
// two shapes — RFC 3339 for anything Verve timestamped, YYYY-MM-DD for anything an
// Account dated — and the axis is a grid of days either way. A value it cannot
// parse is passed through rather than dropped: an event on an odd date is still an
// event that happened.
func dayOf(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(dayLayout)
	}
	return s
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// importLabel names an Import by its file and the Connector that read it. With two
// Connectors, "export.zip" alone no longer says where a history came from, and a
// Connector this build no longer ships falls back to the file it read.
func importLabel(connectorName, sourceFile string) string {
	for _, c := range registry.All() {
		if c.Name() == connectorName {
			return sourceFile + " · " + c.Label()
		}
	}
	return sourceFile
}
