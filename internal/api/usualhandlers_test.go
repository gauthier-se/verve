package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// seedDaily puts one reading of metric on each of the n days before today and, when
// today is set, one on today too.
func seedDaily(t *testing.T, models data.Models, metric string, n int, today bool) {
	t.Helper()
	var ms []data.Measurement
	last := 1
	if today {
		last = 0
	}
	for d := n; d >= last; d-- {
		at := dayAgo(d) + "T06:00:00Z"
		ms = append(ms, data.Measurement{
			Metric: metric, Value: float64(50 + d%7), OriginalUnit: "u",
			StartAt: at, EndAt: at, Source: "Watch", ContentKey: fmt.Sprintf("%s-%d", metric, d),
		})
	}
	seedSteps(t, models, testEmail, ms)
}

func seriesAt(t *testing.T, srv *Server, cookie *http.Cookie, target string) query.Series {
	t.Helper()
	res, body := do(t, srv, target, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var s query.Series
	if err := json.Unmarshal(body["series"], &s); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	return s
}

// TestSeriesCarriesTheUsual: a lone Metric's Series carries each bucket's Usual,
// with or without a Baseline, since the owner reads one value against its past.
func TestSeriesCarriesTheUsual(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDaily(t, models, "resting_heart_rate", 60, false)

	for _, q := range []string{"", "&baseline=previous"} {
		s := seriesAt(t, srv, cookie, "/v1/series?metric=resting_heart_rate&range_preset=7d"+q)
		if len(s.Points) == 0 {
			t.Fatalf("%q: no points", q)
		}
		for _, p := range s.Points {
			if p.Usual == nil || p.Usual.N != 28 {
				t.Errorf("%q: %s: Usual = %+v, want one from 28 days", q, p.Bucket, p.Usual)
			}
		}
	}
}

// TestMultiMetricSeriesCarriesNoUsual: two bands on two axes read as nothing (the
// argument of ADR 0020), so nothing draws it and the engine is not asked for it.
func TestMultiMetricSeriesCarriesNoUsual(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDaily(t, models, "resting_heart_rate", 60, false)
	seedDaily(t, models, "steps", 60, false)

	res, body := do(t, srv, "/v1/series?metric=resting_heart_rate&metric=steps&range_preset=7d", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var list []query.Series
	if err := json.Unmarshal(body["series"], &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, s := range list {
		for _, p := range s.Points {
			if p.Usual != nil {
				t.Errorf("%s %s: Usual = %+v, want none on a multi-Metric read", s.Metric, p.Bucket, *p.Usual)
			}
		}
	}
}

// TestTodayCarriesNoUsual: a custom range may run past today, and today is a day in
// progress. By 10:00 its step total is not below anything, it is unfinished, so the
// bucket holding today is read against nothing, as a Goal never judges it (ADR 0044).
func TestTodayCarriesNoUsual(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDaily(t, models, "steps", 60, true)

	s := seriesAt(t, srv, cookie, fmt.Sprintf(
		"/v1/series?metric=steps&range_preset=custom&range_from=%s&range_to=%s&bucket=day", dayAgo(3), dayAgo(-1)))

	for _, p := range s.Points {
		switch {
		case p.Bucket == dayAgo(0) && p.Usual != nil:
			t.Errorf("today: Usual = %+v, want none on a day in progress", *p.Usual)
		case p.Bucket != dayAgo(0) && p.Usual == nil:
			t.Errorf("%s: no Usual, want one", p.Bucket)
		}
	}
}
