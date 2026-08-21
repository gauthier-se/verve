package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// defaultHistoryMetric is the Metric the long band draws when none is asked for.
// Body mass is the one Metric almost every Account has across its whole history and
// the one a Phase is actually about, which is what makes the bands legible.
const defaultHistoryMetric = "body_mass"

// historyBandView is one Metric drawn over the Account's entire history: a *dense*
// series, one entry per bucket from the first recorded one to the last, gaps
// included and marked.
//
// Dense is the whole point of this view. Everywhere else a bucket without data is
// simply absent, because a gap is never a zero (ADR 0014) and a chart must not draw
// through one. Here the gap *is* the subject — "you changed phones in April 2022 and
// nothing was recorded for five weeks" is a fact about the history — so the grid is
// materialised and each empty bucket says so, rather than leaving the client to
// infer boundaries the server owns (ADR 0012).
type historyBandView struct {
	Metric string        `json:"metric"`
	Unit   string        `json:"unit"`
	Bucket string        `json:"bucket"`
	Points []query.Point `json:"points"`
	// Gaps are the runs of empty buckets, as the first and last bucket key of each
	// run, so the client can shade a span without scanning for one.
	Gaps []historySpanView `json:"gaps"`
	// Phases are the Account's Phases folded onto this grid (ADR 0023), each already
	// clamped to buckets the chart actually draws.
	Phases []historyPhaseView `json:"phases"`
}

// historySpanView is a span on the band's bucket grid, both ends inclusive.
type historySpanView struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// historyPhaseView is one Phase on the grid: its span in buckets, its signed rate,
// and the kind that rate makes it. The kind is derived here rather than in the
// client so "cut" and "bulk" mean the same thing on every screen that says them.
type historyPhaseView struct {
	ID             int64   `json:"id"`
	Kind           string  `json:"kind"`
	RatePctPerWeek float64 `json:"rate_pct_per_week"`
	StartedOn      string  `json:"started_on"`
	EndedOn        *string `json:"ended_on,omitempty"`
	From           string  `json:"from"`
	To             string  `json:"to"`
}

// historyEventView is one dated entry in the ledger. The server says what happened
// and when, with the figures behind it; the words belong to the interface, which is
// why this carries a kind and typed fields rather than a sentence.
type historyEventView struct {
	Kind    string              `json:"kind"`
	Date    string              `json:"date"`
	EndsOn  *string             `json:"ends_on,omitempty"`
	Label   string              `json:"label,omitempty"`
	Body    string              `json:"body,omitempty"`
	Figures []historyFigureView `json:"figures,omitempty"`
	Rate    *float64            `json:"rate_pct_per_week,omitempty"`
}

// historyFigureView is one key/number chip under an event. The key is a stable slug
// the client labels; the value is already in its unit.
type historyFigureView struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// historyView is the whole History page in one call: how far back the data goes,
// the long band, and the dated events that explain its shape.
type historyView struct {
	First  string             `json:"first,omitempty"`
	Last   string             `json:"last,omitempty"`
	Days   int                `json:"days"`
	Band   *historyBandView   `json:"band,omitempty"`
	Events []historyEventView `json:"events"`
}

// Event kinds, in the order they are worth reading when two land on the same day:
// what arrived, then what the Account decided, then what it wrote, then what the
// data itself did.
const (
	eventImport = "import"
	eventPhase  = "phase"
	eventNote   = "note"
	eventSource = "source"
	eventOrigin = "origin"
)

// handleHistory answers the History page: the long view of everything the Account
// holds, and the context that explains it.
//
// It is one call rather than five because the page is one reading. The band's grain,
// the Phase spans folded onto it, the gaps, and the events all have to agree about
// the same axis — and an interface that assembled them from four endpoints would be
// deriving that agreement client-side, which is the thing the read path does not do.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	ctx := r.Context()

	slug := r.URL.Query().Get("metric")
	if slug == "" {
		slug = defaultHistoryMetric
	}
	metric, ok := catalog.Lookup(slug)
	if !ok {
		s.failedValidationResponse(w, r, map[string]string{"metric": unknownMetricMsg})
		return
	}

	span, err := s.models.Measurements.Span(ctx, accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	view := historyView{Events: []historyEventView{}}

	// An Account with no data still gets a page: the events (its own account
	// creation aside, there are none) and an empty band. Nothing here invents a
	// window out of a preset when the real extent is knowable.
	if span.First != "" {
		first, err := time.Parse(time.RFC3339, span.First)
		if err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
		last, err := time.Parse(time.RFC3339, span.Last)
		if err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
		view.First = first.UTC().Format(dayLayout)
		view.Last = last.UTC().Format(dayLayout)
		view.Days = int(last.Sub(first).Hours()/24) + 1

		band, err := s.historyBand(ctx, accountID, metric, first, last)
		if err != nil {
			s.respondSeriesError(w, r, err)
			return
		}
		view.Band = band
	}

	events, err := s.historyEvents(ctx, accountID, span)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	view.Events = events

	if err := writeJSON(w, http.StatusOK, envelope{"history": view}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// historyBand reads the Metric over the whole span and materialises the grid.
func (s *Server) historyBand(ctx context.Context, accountID int64, metric catalog.Metric, first, last time.Time) (*historyBandView, error) {
	// The grain is the span's own: the same rule that gives a Dashboard its bucket,
	// applied to the real extent of the history rather than to a preset. Eight years
	// come out monthly; an Account three months old comes out weekly, which is the
	// right answer for it and not a coarser one borrowed from someone else's history.
	// The bounds are instants — the first and last row's own timestamps — and the
	// grid is a grid of days, so both ends are snapped before anything is enumerated.
	// Without that, a history whose last reading is at 08:00 gets a sixteenth bucket
	// for a fifteen-day span, and it is drawn as a gap.
	first, last = truncateUTCDay(first), truncateUTCDay(last)
	bucket := historyBucket(first, last)
	to := last.AddDate(0, 0, 1) // the window is half-open; include the last day

	series, err := s.engine.Series(ctx, query.Request{
		AccountID: accountID, Metric: metric.Slug,
		From: first, To: to, Bucket: bucket,
	})
	if err != nil {
		return nil, err
	}

	view := &historyBandView{
		Metric: metric.Slug, Unit: series.Unit, Bucket: string(bucket),
		Points: []query.Point{}, Gaps: []historySpanView{}, Phases: []historyPhaseView{},
	}

	byBucket := make(map[string]query.Point, len(series.Points))
	for _, p := range series.Points {
		byBucket[p.Bucket] = p
	}

	grid := bucket.Starts(first, to)
	var run *historySpanView
	for _, key := range grid {
		if p, ok := byBucket[key]; ok {
			view.Points = append(view.Points, p)
			if run != nil {
				view.Gaps = append(view.Gaps, *run)
				run = nil
			}
			continue
		}
		view.Points = append(view.Points, query.Point{Bucket: key, Gap: true})
		if run == nil {
			run = &historySpanView{From: key, To: key}
		} else {
			run.To = key
		}
	}
	if run != nil {
		view.Gaps = append(view.Gaps, *run)
	}

	phases, err := s.models.Phases.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	view.Phases = foldPhases(phases, bucket, grid, last)
	return view, nil
}

// historyBucket picks the band's grain from the span it has to cover, on the same
// thresholds the Dashboard's auto-bucket uses.
func historyBucket(first, last time.Time) query.Bucket {
	days := int(last.Sub(first).Hours() / 24)
	switch {
	case days <= 31:
		return query.Day
	case days <= 366:
		return query.Week
	default:
		return query.Month
	}
}

// foldPhases places each Phase on the band's grid, dropping the ones that fall
// entirely outside it and clamping the ones that overlap it at either end. An open
// Phase runs to the last bucket, which is where "still cutting" is drawn.
func foldPhases(phases []data.Phase, bucket query.Bucket, grid []string, last time.Time) []historyPhaseView {
	out := []historyPhaseView{}
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
			if e, err := time.Parse(dayLayout, *p.EndedAt); err == nil {
				ended = e
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
		out = append(out, historyPhaseView{
			ID: p.ID, Kind: phaseKind(p.RatePctPerWeek), RatePctPerWeek: p.RatePctPerWeek,
			StartedOn: p.StartedAt, EndedOn: p.EndedAt, From: from, To: to,
		})
	}
	return out
}

// phaseKind names a Phase by the sign of its rate: the same three words the Plan
// page uses, derived from the one number that decides them.
func phaseKind(rate float64) string {
	switch {
	case rate > 0:
		return "bulk"
	case rate < 0:
		return "cut"
	default:
		return "maintenance"
	}
}

// historyEvents gathers every dated event into one list, most recent first.
func (s *Server) historyEvents(ctx context.Context, accountID int64, span data.Span) ([]historyEventView, error) {
	events := []historyEventView{}

	imports, err := s.models.Measurements.ListImports(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, imp := range imports {
		day := imp.ImportedAt
		if t, err := time.Parse(time.RFC3339, imp.ImportedAt); err == nil {
			day = t.UTC().Format(dayLayout)
		}
		events = append(events, historyEventView{
			Kind: eventImport, Date: day, Label: imp.SourceFile,
			Figures: []historyFigureView{
				{Key: "added", Value: float64(imp.AddedCount)},
				{Key: "skipped", Value: float64(imp.SkippedCount)},
				{Key: "unmapped", Value: float64(imp.UnmappedCount)},
			},
		})
	}

	phases, err := s.models.Phases.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, p := range phases {
		rate := p.RatePctPerWeek
		events = append(events, historyEventView{
			Kind: eventPhase, Date: p.StartedAt, EndsOn: p.EndedAt,
			Label: phaseKind(rate), Rate: &rate,
		})
	}

	notes, err := s.models.Annotations.ListAll(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		events = append(events, historyEventView{
			Kind: eventNote, Date: n.StartsOn, EndsOn: n.EndsOn,
			Label: n.Label, Body: valueOrEmpty(n.Body),
		})
	}

	sources, err := s.models.Measurements.Sources(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, src := range sources {
		day := src.FirstSeen
		if t, err := time.Parse(time.RFC3339, src.FirstSeen); err == nil {
			day = t.UTC().Format(dayLayout)
		}
		events = append(events, historyEventView{
			Kind: eventSource, Date: day, Label: src.Source,
			Figures: []historyFigureView{{Key: "rows", Value: float64(src.Rows)}},
		})
	}

	// The origin: the oldest thing the Account holds. It closes the list because it
	// is where the history starts, and it is stated rather than implied — the first
	// bucket of a chart is not the same claim as "this is the earliest record you
	// have, and nothing before it was dropped".
	if span.First != "" {
		day := span.First
		if t, err := time.Parse(time.RFC3339, span.First); err == nil {
			day = t.UTC().Format(dayLayout)
		}
		unmapped, err := s.models.Measurements.CountUnmapped(ctx, accountID)
		if err != nil {
			return nil, err
		}
		event := historyEventView{Kind: eventOrigin, Date: day}
		if unmapped > 0 {
			event.Figures = []historyFigureView{{Key: "unmapped_kept", Value: float64(unmapped)}}
		}
		events = append(events, event)
	}

	// Newest first, and on a tie the kind order above, so a day carrying an import
	// and the Phase it revealed reads in that order rather than at random.
	kindRank := map[string]int{eventImport: 0, eventPhase: 1, eventNote: 2, eventSource: 3, eventOrigin: 4}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Date != events[j].Date {
			return events[i].Date > events[j].Date
		}
		return kindRank[events[i].Kind] < kindRank[events[j].Kind]
	})
	return events, nil
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// truncateUTCDay snaps an instant to the start of its UTC day.
func truncateUTCDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
