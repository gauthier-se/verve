// Package timeaxis resolves a Dashboard's time axis — Time range, optional Baseline,
// effective bucket — from the stored tokens, server-side (CONTEXT.md: Time axis;
// ADR 0015). Pure and DB-free: one test surface for every preset, rule, and bucket.
package timeaxis

import (
	"time"

	"github.com/gauthier-se/verve/internal/query"
)

// allFloor is the lower bound the "all" preset expands to — earlier than any
// plausible history, so its span always resolves to a month bucket.
const allFloor = "2000-01-01"

const dayLayout = "2006-01-02"

// Tokens is a Dashboard's temporal state as persisted and as the SPA forwards it.
// Range and baseline bounds are day-granularity YYYY-MM-DD and meaningful only for
// the "custom" preset/rule; Bucket is a Panel's optional override.
type Tokens struct {
	RangePreset  string
	RangeFrom    *string
	RangeTo      *string
	BaselineRule string
	BaselineFrom *string
	BaselineTo   *string
	Bucket       *string
}

// Window is a resolved half-open time range [From, To).
type Window struct {
	From time.Time
	To   time.Time
}

// Resolved is a Tokens set turned concrete: the current Window, the bucket to
// query at, and the Baseline Window — nil when comparison is off or the range is
// "all".
type Resolved struct {
	Current  Window
	Bucket   query.Bucket
	Baseline *Window
}

// Invalid is a set of field-keyed validation messages; empty means valid. It
// implements error so Resolve/Validate can return it directly, and the API maps
// its fields to a 422.
type Invalid map[string]string

func (v Invalid) Error() string {
	for f, m := range v {
		return f + ": " + m
	}
	return "invalid"
}

var rangePresets = map[string]bool{
	"7d": true, "30d": true, "3m": true, "1y": true, "all": true, "custom": true,
}

var baselineRules = map[string]bool{
	"none": true, "previous": true, "same_period_last_year": true, "custom": true,
}

// Validate checks a Tokens set without resolving it (no clock) — the shape a
// Dashboard persists: known preset/rule, ordered bounds for "custom", no baseline
// bounds on a non-custom rule, a day/week/month override. Returns nil or an Invalid.
func Validate(t Tokens) error {
	v := Invalid{}
	validateRange(v, t)
	validateBaseline(v, t)
	validateOverride(v, t.Bucket)
	if len(v) == 0 {
		return nil
	}
	return v
}

// Resolve validates the tokens, then resolves them against now: presets end at
// today (UTC midnight), "all" starts at allFloor and carries no Baseline (a rule
// with "all" is an error), and an override bucket wins over the span-derived one.
func Resolve(t Tokens, now time.Time) (Resolved, error) {
	if err := Validate(t); err != nil {
		return Resolved{}, err
	}
	if t.RangePreset == "all" && comparing(t.BaselineRule) {
		return Resolved{}, Invalid{"baseline": "comparison is not available for the all range"}
	}

	cur, err := rangeWindow(t, now)
	if err != nil {
		return Resolved{}, err
	}

	bucket := autoBucket(cur)
	if t.Bucket != nil {
		b, _ := query.ParseBucket(*t.Bucket) // validated above
		bucket = b
	}

	res := Resolved{Current: cur, Bucket: bucket}
	if comparing(t.BaselineRule) {
		bw := baselineWindow(t, cur)
		res.Baseline = &bw
	}
	return res, nil
}

func comparing(rule string) bool { return rule != "" && rule != "none" }

// rangeWindow resolves the current [from, to): to is today at UTC midnight for
// every preset, from is shifted back by the preset (calendar-aware); "custom" uses
// the given bounds, "all" starts at allFloor.
func rangeWindow(t Tokens, now time.Time) (Window, error) {
	today := truncateDay(now)
	switch t.RangePreset {
	case "7d":
		return Window{today.AddDate(0, 0, -7), today}, nil
	case "30d":
		return Window{today.AddDate(0, 0, -30), today}, nil
	case "3m":
		return Window{today.AddDate(0, -3, 0), today}, nil
	case "1y":
		return Window{today.AddDate(-1, 0, 0), today}, nil
	case "all":
		floor, _ := time.Parse(dayLayout, allFloor)
		return Window{floor.UTC(), today}, nil
	default: // custom
		from, _ := parseDay(*t.RangeFrom)
		to, _ := parseDay(*t.RangeTo)
		return Window{from, to}, nil
	}
}

// baselineWindow derives the Baseline [from, to) from the current window: previous
// shifts back by its length, same_period_last_year by one year (AddDate normalizes
// Feb 29 → Mar 1), custom uses the absolute bounds.
func baselineWindow(t Tokens, cur Window) Window {
	switch t.BaselineRule {
	case "previous":
		span := cur.To.Sub(cur.From)
		return Window{cur.From.Add(-span), cur.To.Add(-span)}
	case "same_period_last_year":
		return Window{cur.From.AddDate(-1, 0, 0), cur.To.AddDate(-1, 0, 0)}
	default: // custom
		from, _ := parseDay(*t.BaselineFrom)
		to, _ := parseDay(*t.BaselineTo)
		return Window{from, to}
	}
}

// autoBucket derives the bucket from a span: ≤31d→day, ≤366d→week, else month,
// keeping the point count bounded without a per-Panel choice.
func autoBucket(w Window) query.Bucket {
	days := int(w.To.Sub(w.From) / (24 * time.Hour))
	switch {
	case days <= 31:
		return query.Day
	case days <= 366:
		return query.Week
	default:
		return query.Month
	}
}

func validateRange(v Invalid, t Tokens) {
	if !rangePresets[t.RangePreset] {
		v["range_preset"] = "must be one of 7d, 30d, 3m, 1y, all, custom"
		return
	}
	if t.RangePreset != "custom" {
		return
	}
	validateBounds(v, t.RangeFrom, t.RangeTo, "range_from", "range_to", "a custom range")
}

func validateBaseline(v Invalid, t Tokens) {
	// An empty rule is the zero-value synonym for "none": comparison off.
	if t.BaselineRule != "" && !baselineRules[t.BaselineRule] {
		v["baseline_rule"] = "must be one of none, previous, same_period_last_year, custom"
		return
	}
	if t.BaselineRule != "custom" {
		if t.BaselineFrom != nil || t.BaselineTo != nil {
			v["baseline_from"] = "baseline bounds are only for the custom rule"
		}
		return
	}
	validateBounds(v, t.BaselineFrom, t.BaselineTo, "baseline_from", "baseline_to", "a custom baseline")
}

// validateBounds checks a custom window: both bounds present, YYYY-MM-DD, ordered.
func validateBounds(v Invalid, from, to *string, fromField, toField, what string) {
	if from == nil || to == nil {
		v[fromField] = what + " needs both " + fromField + " and " + toField
		return
	}
	f, okF := parseDay(*from)
	tt, okT := parseDay(*to)
	if !okF {
		v[fromField] = "must be YYYY-MM-DD"
	}
	if !okT {
		v[toField] = "must be YYYY-MM-DD"
	}
	if okF && okT && !tt.After(f) {
		v[toField] = "must be after " + fromField
	}
}

func validateOverride(v Invalid, bucket *string) {
	if bucket == nil {
		return
	}
	if _, err := query.ParseBucket(*bucket); err != nil {
		v["bucket"] = "must be day, week, or month, or omitted to auto-derive"
	}
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse(dayLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
