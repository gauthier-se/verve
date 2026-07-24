package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// daysAgo renders an RFC 3339 UTC timestamp n days before now, for seeding
// measurements against the Ledger's fixed now-relative windows.
func daysAgo(n int) string {
	return time.Now().UTC().AddDate(0, 0, -n).Format(time.RFC3339)
}

// findRow returns the Ledger row for a metric slug, failing if it is absent.
func findRow(t *testing.T, rows []ledgerRow, metric string) ledgerRow {
	t.Helper()
	for _, r := range rows {
		if r.Metric == metric {
			return r
		}
	}
	t.Fatalf("ledger rows %+v carry no %q row", rows, metric)
	return ledgerRow{}
}

func TestLedgerFoldsWindowsAndWeekDelta(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	// A `sum` Metric: 700 steps this week (2 days ago) and 350 the week before
	// (9 days ago). Week figure is the 7-day total ÷ 7 = 100/day; the previous
	// week is 350 ÷ 7 = 50/day, so the delta is +50/day (+100%). The 30-day window
	// holds both: 1050 ÷ 30 = 35/day.
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 700, OriginalUnit: "count", StartAt: daysAgo(2), EndAt: daysAgo(2), Source: "Watch", ContentKey: "s1"},
		{Metric: "steps", Value: 350, OriginalUnit: "count", StartAt: daysAgo(9), EndAt: daysAgo(9), Source: "Watch", ContentKey: "s2"},
	})

	res, body := do(t, srv, "/v1/ledger", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var rows []ledgerRow
	if err := json.Unmarshal(body["rows"], &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}

	steps := findRow(t, rows, "steps")
	if steps.Unit != "count" || steps.Aggregation != "sum" {
		t.Errorf("steps row = %+v, want unit count / sum", steps)
	}
	assertFloat(t, "week", steps.Week, 100)
	assertFloat(t, "month", steps.Month, 35)
	assertFloat(t, "delta_abs", steps.DeltaAbs, 50)
	assertFloat(t, "delta_pct", steps.DeltaPct, 100)
	// Latest is the most recent daily bucket: 700 steps two days ago.
	if steps.Latest == nil || steps.Latest.Value != 700 {
		t.Fatalf("latest = %+v, want value 700", steps.Latest)
	}
	if steps.Latest.Date != time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02") {
		t.Errorf("latest date = %q, want two days ago", steps.Latest.Date)
	}
}

func TestLedgerOnlyListsMetricsWithData(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "body_mass", Value: 78.4, OriginalUnit: "kg", StartAt: daysAgo(1), EndAt: daysAgo(1), Source: "Health", ContentKey: "m1"},
	})

	_, body := do(t, srv, "/v1/ledger", cookie)
	var rows []ledgerRow
	if err := json.Unmarshal(body["rows"], &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	// Only the one seeded Metric appears — no empty rows for the rest of the Catalog.
	if len(rows) != 1 || rows[0].Metric != "body_mass" {
		t.Fatalf("rows = %+v, want only body_mass", rows)
	}
	// A `latest` Metric is not divided by the window; its figure is the value itself.
	assertFloat(t, "week", rows[0].Week, 78.4)
	if rows[0].Latest == nil || rows[0].Latest.Value != 78.4 {
		t.Errorf("latest = %+v, want 78.4", rows[0].Latest)
	}
}

func TestLedgerRequiresAuth(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res, _ := do(t, srv, "/v1/ledger")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}

// assertFloat fails unless got is non-nil and within a small epsilon of want.
func assertFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}
