package query

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
)

// Which rows a read is allowed to see.
//
// Verve keeps every Measurement from every Source and resolves the overlap when a
// graph needs one series (ADR 0003). This file is that resolution: electing the
// winning imported Source per day (ADR 0034), and letting a Manual entry displace
// the winner on the days the Account typed one (ADR 0022).
//
// Both mechanics resolve at day grain, which is the grain at which the evidence
// actually changes, and never at the caller's bucket grain: a daily chart, a
// monthly chart and a window summary must not disagree about which rows are in
// play.
//
// Sleep runs a second election, over Nights rather than days, in sleep.go. It is
// not a copy of this one: its candidates are ranked by the richness of their
// evidence first (a Source with Stages beats one with only in-bed time, ADR 0027),
// and its winner filters rows in Go rather than becoming a SQL predicate, because
// a Night is not a range of start_at. What the two share is the ranking itself,
// catalog.ResolveSource, and the reduction of many per-period winners to the one
// name a Series reports: reportedSource, below.

// sourceFilter is the resolved row set for one Metric over one window: the winning
// imported Source per day (ADR 0003, ADR 0034) plus, when the Account has typed
// values for this Metric, the Manual overlay (ADR 0022).
//
// Both mechanics resolve at **day** grain, and for the same reason. Source priority
// elects the winner among the Sources that recorded something *that day*: a Source
// that covers part of a history must not win the days it never saw, or importing a
// three-month mirror of an Apple export blanks the years around it. A human corrects
// isolated days, so a Manual entry cannot compete as a Source at all: ranking it
// first would make one typed value the winner of everything, and it overlays
// instead: on a day the Account typed a value, that day's Manual rows replace the
// winner's rows.
//
// Day grain, rather than the caller's bucket grain, is what keeps the resolved row set
// independent of how the caller happens to be bucketing — so a daily chart, a monthly
// chart and a window summary can never disagree about which rows are in play.
type sourceFilter struct {
	source    string // dominant imported Source; "" when only Manual rows exist
	runs      []sourceRun
	hasManual bool // the Account has Manual rows for this Metric in the window
}

// sourceRun is one stretch of days a single Source won, as a half-open instant
// range. Runs exist only when the window's days did not all elect the same Source;
// one winner everywhere leaves them nil and the predicate stays the plain equality
// it has always been.
//
// Consecutive days electing the same Source merge into one run, and merging across
// days nobody recorded is safe: a day with no rows selects nothing whichever run
// spans it. So the case this is built for, years of one export with another's
// shorter mirror inside them, is three ranges rather than four thousand, and the
// bounds stay RFC 3339 prefixes compared against start_at, which the
// (account_id, metric, start_at) index still serves.
type sourceRun struct {
	source   string
	from, to string // RFC 3339 UTC, to exclusive
}

// any reports whether the filter selects anything at all: an imported winner, Manual
// rows, or both. False means no data in the range.
func (f sourceFilter) any() bool { return f.source != "" || f.hasManual }

// reported is the Source name carried on the Series. An active overlay does not change
// it: the imported Source is still what the bulk of the curve comes from, and calling
// the whole series "Manual" because one day was corrected would misdescribe it.
func (f sourceFilter) reported() string {
	if f.source != "" {
		return f.source
	}
	return catalog.SourceManual
}

// where renders the row-set predicate shared by every read path, and its args. It is a
// drop-in replacement for the plain `source = ?` filter this engine used before the
// overlay existed.
//
// With no Manual rows — every Metric of every Account that has never typed one — it
// emits exactly that original predicate, so nothing existing can change behaviour.
func (f sourceFilter) where(req Request) (string, []any) {
	const base = `account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?`
	from, to := rfc3339(req.From), rfc3339(req.To)

	if !f.hasManual && len(f.runs) == 0 {
		return base, []any{req.AccountID, req.Metric, f.source, from, to}
	}
	if f.source == "" {
		// Manual rows only: the base predicate, pinned to the Manual Source.
		return base, []any{req.AccountID, req.Metric, catalog.SourceManual, from, to}
	}

	win, winArgs := f.winner()
	if !f.hasManual {
		args := append([]any{req.AccountID, req.Metric}, winArgs...)
		return `account_id = ? AND metric = ? AND ` + win + ` AND start_at >= ? AND start_at < ?`,
			append(args, from, to)
	}

	// The overlay. The manual-days subquery is deliberately *not* range-filtered: a
	// window boundary that splits a day would otherwise let the device's rows for that
	// day survive alongside the correction. Manual rows are few by nature (a person
	// types them), so scanning them all is cheaper than getting this subtly wrong.
	overlay := `account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?
		AND (` + win + ` OR source = ?)
		AND (source = ? OR date(start_at) NOT IN (
			SELECT date(mo.start_at) FROM measurements mo
			WHERE mo.account_id = ? AND mo.metric = ? AND mo.source = ?))`
	args := []any{req.AccountID, req.Metric, from, to}
	args = append(args, winArgs...)
	return overlay, append(args,
		catalog.SourceManual,
		catalog.SourceManual,
		req.AccountID, req.Metric, catalog.SourceManual,
	)
}

// winner renders "the rows of whichever Source won", and its args: one equality when
// a single Source won every day of the window, a disjunction of dated runs when
// several did. The single-winner shape is the predicate this engine has always
// emitted, byte for byte, which is every Account that holds one export.
func (f sourceFilter) winner() (string, []any) {
	if len(f.runs) == 0 {
		return `source = ?`, []any{f.source}
	}
	parts := make([]string, 0, len(f.runs))
	args := make([]any, 0, len(f.runs)*3)
	for _, r := range f.runs {
		parts = append(parts, `(source = ? AND start_at >= ? AND start_at < ?)`)
		args = append(args, r.source, r.from, r.to)
	}
	return `(` + strings.Join(parts, " OR ") + `)`, args
}

// resolveSource finds the Sources with data for the Metric in the range, elects the
// single imported winner by the Metric's Source priority (ADR 0003), and notes
// separately whether the Account has Manual rows to overlay (ADR 0022). Manual is
// split out *before* the election so it never competes as a Source.
func (e Engine) resolveSource(ctx context.Context, req Request) (sourceFilter, error) {
	// Which Sources recorded something, day by day. It is one row per (day, Source)
	// rather than one per Source, and the index that served the old query serves this
	// one: the extra work is a date() per distinct pair, not per row.
	const q = `
		SELECT DISTINCT date(start_at) AS d, source
		FROM measurements
		WHERE account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?
		ORDER BY d`
	rows, err := e.DB.QueryContext(ctx, q, req.AccountID, req.Metric, rfc3339(req.From), rfc3339(req.To))
	if err != nil {
		return sourceFilter{}, fmt.Errorf("query: distinct sources: %w", err)
	}
	defer rows.Close()

	var f sourceFilter
	var days []string
	byDay := map[string][]string{}
	for rows.Next() {
		var day sql.NullString
		var s string
		if err := rows.Scan(&day, &s); err != nil {
			return sourceFilter{}, fmt.Errorf("query: scan source: %w", err)
		}
		if s == catalog.SourceManual {
			f.hasManual = true
			continue
		}
		// A start_at no Connector could parse has no day, and no bucket either: it is
		// already absent from every series, so it is absent from the election too.
		if !day.Valid {
			continue
		}
		if _, seen := byDay[day.String]; !seen {
			days = append(days, day.String)
		}
		byDay[day.String] = append(byDay[day.String], s)
	}
	if err := rows.Err(); err != nil {
		return sourceFilter{}, fmt.Errorf("query: iterate sources: %w", err)
	}

	f.runs = electRuns(req.Metric, days, byDay)
	switch {
	case len(f.runs) == 0:
		// No imported rows at all; Manual may still carry the window.
	case sameSource(f.runs):
		// One Source won every day, which is every Account holding one export: keep
		// the whole-window shape so nothing about the query changes.
		f.source = f.runs[0].source
		f.runs = nil
	default:
		f.source = reportedSource(req.Metric, runSources(f.runs))
	}
	return f, nil
}

// electRuns elects a winning Source per day and merges consecutive days that elected
// the same one into runs (ADR 0034). days must be sorted.
func electRuns(slug string, days []string, byDay map[string][]string) []sourceRun {
	runs := []sourceRun{}
	for _, d := range days {
		candidates := byDay[d]
		sort.Strings(candidates)
		winner, ok := catalog.ResolveSource(slug, candidates)
		if !ok {
			continue
		}
		end := dayStart(nextDay(d))
		if n := len(runs); n > 0 && runs[n-1].source == winner {
			runs[n-1].to = end
			continue
		}
		runs = append(runs, sourceRun{source: winner, from: dayStart(d), to: end})
	}
	return runs
}

// dayStart renders a YYYY-MM-DD day as the RFC 3339 instant it begins at, so a run's
// bounds compare against start_at as plain strings.
func dayStart(day string) string { return day + "T00:00:00Z" }

// nextDay is the day after a YYYY-MM-DD day, or the day itself if it is unreadable
// (which cannot happen: SQLite's date() produced it).
func nextDay(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// sameSource reports whether every run was won by the same Source.
func sameSource(runs []sourceRun) bool {
	for _, r := range runs[1:] {
		if r.source != runs[0].source {
			return false
		}
	}
	return true
}

// runSources is the distinct Sources that won at least one run.
func runSources(runs []sourceRun) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range runs {
		if !seen[r.source] {
			seen[r.source] = true
			out = append(out, r.source)
		}
	}
	sort.Strings(out)
	return out
}

// reportedSource reduces the winners of a per-period election to the one Source
// name a Series carries.
//
// It ranks them by the same priority the election itself used, so the reported
// name is the dominant evidence rather than whichever period happened to be last.
//
// Both elections end here: the per-day one over Measurements in this file, and the
// per-Night one over States in sleep.go. That is deliberate and is the whole of
// what they share. Everything before this point differs, because the evidence
// differs: a day's candidates are simply the Sources that recorded something, a
// Night's are ranked by whether they carry Stages at all (ADR 0027).
func reportedSource(slug string, winners []string) string {
	source, _ := catalog.ResolveSource(slug, winners)
	return source
}

// aggregate runs the per-bucket SQL for the Metric's rule against the resolved row
// set and returns the ordered buckets. The filter has already settled which rows are
// in play (winning Source, Manual overlay), so every rule below is unchanged by the
// overlay's existence — that separation is the point of resolving at day grain.
