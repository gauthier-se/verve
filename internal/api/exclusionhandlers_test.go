package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
)

// seedRows inserts Measurements for the named Account, one per (day, source), so a
// test can say exactly which rows a purge should and should not reach.
func seedRows(t *testing.T, models data.Models, email, metric, source string, days []string) {
	t.Helper()
	acc, err := models.Accounts.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	rows := make([]data.Measurement, 0, len(days))
	for i, day := range days {
		at := day
		if len(day) == 10 { // a bare day: put it at noon, away from either boundary
			at = day + "T12:00:00Z"
		}
		rows = append(rows, data.Measurement{
			AccountID: acc.ID, Metric: metric, Value: float64(20 + i), OriginalUnit: "%",
			StartAt: at, EndAt: at, Source: source,
			ContentKey: fmt.Sprintf("%s-%s-%s", metric, source, at),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

// countRows is what the store actually holds for a Metric, read straight from the
// model rather than through the API: the assertions are about rows, not about what
// an endpoint chooses to report.
func countRows(t *testing.T, models data.Models, email, metric string) int64 {
	t.Helper()
	acc, err := models.Accounts.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	n, err := models.Measurements.CountInSpan(context.Background(), acc.ID, metric, "", "")
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// exclude posts an Exclusion and returns the status and the resulting view.
func exclude(t *testing.T, srv *Server, cookie *http.Cookie, body string) (int, exclusionView) {
	t.Helper()
	res, decoded := doReq(t, srv, http.MethodPost, "/v1/exclusions", body, cookie)
	var view exclusionView
	if raw, ok := decoded["exclusion"]; ok {
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatalf("decode exclusion: %v", err)
		}
	}
	return res.StatusCode, view
}

func listExclusions(t *testing.T, srv *Server, cookie *http.Cookie) []exclusionView {
	t.Helper()
	res, body := doReq(t, srv, http.MethodGet, "/v1/exclusions", "", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list exclusions status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var views []exclusionView
	if err := json.Unmarshal(body["exclusions"], &views); err != nil {
		t.Fatalf("decode exclusions: %v", err)
	}
	return views
}

func TestExclusionsRequireAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, "/v1/exclusions", ""},
		{http.MethodGet, "/v1/exclusions/preview?metric=body_fat_percentage", ""},
		{http.MethodPost, "/v1/exclusions", `{"metric":"body_fat_percentage"}`},
		{http.MethodDelete, "/v1/exclusions/1", ""},
	} {
		res, _ := doReq(t, srv, tc.method, tc.target, tc.body)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s %s status = %d, want 401", tc.method, tc.target, res.StatusCode)
		}
	}
}

func TestListExclusionsEmpty(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	if got := listExclusions(t, srv, cookie); len(got) != 0 {
		t.Errorf("new account has %d exclusions, want 0", len(got))
	}
}

// An unbounded Exclusion is the common case: everything this Metric holds, gone, and
// nothing of any other Metric touched.
func TestExcludeWholeMetricPurgesIt(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life",
		[]string{"2025-06-01", "2026-01-15", "2026-03-02"})
	seedRows(t, models, testEmail, "body_mass", "Zepp Life", []string{"2026-01-15"})

	status, view := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", status)
	}
	if view.Purged != 3 {
		t.Errorf("purged = %d, want 3", view.Purged)
	}
	if view.StartsOn != "" || view.EndsOn != "" {
		t.Errorf("span = %q..%q, want unbounded on both sides", view.StartsOn, view.EndsOn)
	}
	if n := countRows(t, models, testEmail, "body_fat_percentage"); n != 0 {
		t.Errorf("body_fat_percentage rows left = %d, want 0", n)
	}
	if n := countRows(t, models, testEmail, "body_mass"); n != 1 {
		t.Errorf("body_mass rows = %d, want 1 untouched", n)
	}
}

// The bounded case, which is the one the owner actually asked for: everything except
// body fat since 2026. What precedes the bound stays.
func TestExcludeFromADayPurgesOnlyFromThere(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life",
		[]string{"2025-12-30", "2025-12-31", "2026-01-01", "2026-07-04"})

	status, view := exclude(t, srv, cookie, `{"metric":"body_fat_percentage","starts_on":"2026-01-01"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", status)
	}
	if view.Purged != 2 {
		t.Errorf("purged = %d, want 2", view.Purged)
	}
	if n := countRows(t, models, testEmail, "body_fat_percentage"); n != 2 {
		t.Errorf("rows left = %d, want the 2 before the bound", n)
	}
}

// Both bounds are inclusive, and the day is the UTC day every other read buckets by:
// a row at 23:00 on the last day is inside, one at 00:00 the next day is not.
func TestExclusionSpanBoundsAreInclusiveDays(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life", []string{
		"2026-02-28T23:59:59Z", // the day before starts_on: out
		"2026-03-01T00:00:00Z", // starts_on, first instant: in
		"2026-03-31T23:00:00Z", // ends_on, last hour: in
		"2026-04-01T00:00:00Z", // the day after ends_on: out
	})

	status, view := exclude(t, srv, cookie,
		`{"metric":"body_fat_percentage","starts_on":"2026-03-01","ends_on":"2026-03-31"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", status)
	}
	if view.Purged != 2 {
		t.Errorf("purged = %d, want 2 (the two days inside the span)", view.Purged)
	}
	if n := countRows(t, models, testEmail, "body_fat_percentage"); n != 2 {
		t.Errorf("rows left = %d, want the 2 outside the span", n)
	}
}

// "Delete every body fat value" means every one. A Manual entry in the span goes
// with the rest, which is the one thing the export cannot bring back.
func TestExclusionPurgesManualEntriesToo(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life", []string{"2026-01-10"})
	seedRows(t, models, testEmail, "body_fat_percentage", catalog.SourceManual, []string{"2026-01-11"})

	status, view := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", status)
	}
	if view.Purged != 2 {
		t.Errorf("purged = %d, want 2 including the Manual entry", view.Purged)
	}
}

// An Exclusion governs Connectors, never the Account: typing the figure you trust on
// a Metric whose device stream you refused is the point, not an oversight.
func TestManualEntryStillAcceptedOnExcludedMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	if status, _ := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`); status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", status)
	}
	res, body := doReq(t, srv, http.MethodPost, "/v1/measurements",
		`{"metric":"body_fat_percentage","value":0.21,"measured_at":"2026-02-01T08:00:00Z"}`, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("manual entry status = %d, want 201 (%s)", res.StatusCode, body["error"])
	}
}

// Saying the same thing twice is not an error, and must not report a second purge:
// the rows went the first time, and 200 says the state asked for already holds.
func TestExcludeTwiceIsIdempotent(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life", []string{"2026-01-10", "2026-01-11"})

	status, first := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`)
	if status != http.StatusCreated || first.Purged != 2 {
		t.Fatalf("first create = %d purging %d, want 201 purging 2", status, first.Purged)
	}
	status, second := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`)
	if status != http.StatusOK {
		t.Errorf("second create status = %d, want 200", status)
	}
	if second.ID != first.ID {
		t.Errorf("second create id = %d, want the standing row %d", second.ID, first.ID)
	}
	if second.Purged != 2 {
		t.Errorf("second create purged = %d, want the original 2 rather than a fresh count", second.Purged)
	}
	if got := listExclusions(t, srv, cookie); len(got) != 1 {
		t.Errorf("list has %d exclusions, want 1", len(got))
	}
}

// The same Metric excluded over two different spans is two rules, not a duplicate.
func TestExcludeSameMetricDifferentSpans(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	if status, _ := exclude(t, srv, cookie, `{"metric":"body_fat_percentage","ends_on":"2025-12-31"}`); status != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", status)
	}
	if status, _ := exclude(t, srv, cookie, `{"metric":"body_fat_percentage","starts_on":"2026-06-01"}`); status != http.StatusCreated {
		t.Fatalf("second create status = %d, want 201", status)
	}
	if got := listExclusions(t, srv, cookie); len(got) != 2 {
		t.Errorf("list has %d exclusions, want 2", len(got))
	}
}

func TestExcludeRejectsUnusableMetrics(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	for name, body := range map[string]string{
		"unknown":      `{"metric":"not_a_metric"}`,
		"missing":      `{"metric":""}`,
		"derived":      `{"metric":"calorie_balance"}`,
		"state-backed": `{"metric":"sleep"}`,
		// Training volume is folded from the Sessions family, so the same refusal
		// covers it: the check asks the rule rather than naming sleep (ADR 0040).
		"session-backed": `{"metric":"training_time"}`,
		"bad start":      `{"metric":"steps","starts_on":"01/01/2026"}`,
		"bad end":        `{"metric":"steps","ends_on":"2026-13-40"}`,
		"inverted span":  `{"metric":"steps","starts_on":"2026-03-31","ends_on":"2026-03-01"}`,
	} {
		t.Run(name, func(t *testing.T) {
			res, _ := doReq(t, srv, http.MethodPost, "/v1/exclusions", body, cookie)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", res.StatusCode)
			}
		})
	}
}

// A refusal must leave nothing behind: a rejected Exclusion has no rule and no purge.
func TestRejectedExclusionPurgesNothing(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "steps", "Watch", []string{"2026-01-10"})

	res, _ := doReq(t, srv, http.MethodPost, "/v1/exclusions",
		`{"metric":"steps","starts_on":"2026-03-31","ends_on":"2026-03-01"}`, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	if n := countRows(t, models, testEmail, "steps"); n != 1 {
		t.Errorf("steps rows = %d, want 1 untouched", n)
	}
	if got := listExclusions(t, srv, cookie); len(got) != 0 {
		t.Errorf("list has %d exclusions, want 0", len(got))
	}
}

// The preview counts exactly what the purge then removes: a confirmation showing a
// different number than the delete matches would be an estimate, not a confirmation.
func TestPreviewCountsWhatThePurgeRemoves(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life",
		[]string{"2025-12-31", "2026-01-01", "2026-02-02"})

	res, body := doReq(t, srv, http.MethodGet,
		"/v1/exclusions/preview?metric=body_fat_percentage&starts_on=2026-01-01", "", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var previewed int64
	if err := json.Unmarshal(body["measurements"], &previewed); err != nil {
		t.Fatalf("decode measurements: %v", err)
	}
	if previewed != 2 {
		t.Fatalf("preview = %d, want 2", previewed)
	}
	if got := listExclusions(t, srv, cookie); len(got) != 0 {
		t.Errorf("preview wrote %d exclusions, want none", len(got))
	}

	_, view := exclude(t, srv, cookie, `{"metric":"body_fat_percentage","starts_on":"2026-01-01"}`)
	if view.Purged != previewed {
		t.Errorf("purged %d, previewed %d: the two must be the same predicate", view.Purged, previewed)
	}
}

func TestPreviewRejectsUnusableMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := doReq(t, srv, http.MethodGet, "/v1/exclusions/preview?metric=sleep", "", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", res.StatusCode)
	}
}

// Deleting an Exclusion removes the rule and restores nothing: the rows are gone,
// and what brings the imported ones back is the next import.
func TestDeleteExclusionRestoresNothing(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life", []string{"2026-01-10"})

	_, view := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`)
	res, body := doReq(t, srv, http.MethodDelete, "/v1/exclusions/"+itoa(view.ID), "", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (%s)", res.StatusCode, body["error"])
	}
	if got := listExclusions(t, srv, cookie); len(got) != 0 {
		t.Errorf("list has %d exclusions after delete, want 0", len(got))
	}
	if n := countRows(t, models, testEmail, "body_fat_percentage"); n != 0 {
		t.Errorf("rows = %d, want 0: deleting the rule restores nothing", n)
	}
}

func TestDeleteUnknownExclusion(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := doReq(t, srv, http.MethodDelete, "/v1/exclusions/4242", "", cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

// One Account's Exclusion is invisible to another, deletes nothing of another's, and
// purges nothing of another's: the isolation is per row, not per endpoint.
func TestExclusionsAreAccountScoped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	const otherEmail, otherPassword = "other@example.test", "correct-horse-battery-staple"
	seedAccountWithPassword(t, models, otherEmail, otherPassword)
	otherCookie := login(t, srv, otherEmail, otherPassword)

	seedRows(t, models, testEmail, "body_fat_percentage", "Zepp Life", []string{"2026-01-10"})
	seedRows(t, models, otherEmail, "body_fat_percentage", "Zepp Life", []string{"2026-01-10", "2026-01-11"})

	_, view := exclude(t, srv, cookie, `{"metric":"body_fat_percentage"}`)
	if view.Purged != 1 {
		t.Errorf("purged = %d, want 1: only this Account's row", view.Purged)
	}
	if n := countRows(t, models, otherEmail, "body_fat_percentage"); n != 2 {
		t.Errorf("other Account has %d rows, want 2 untouched", n)
	}
	if got := listExclusions(t, srv, otherCookie); len(got) != 0 {
		t.Errorf("other Account sees %d exclusions, want 0", len(got))
	}
	res, _ := doReq(t, srv, http.MethodDelete, "/v1/exclusions/"+itoa(view.ID), "", otherCookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account delete status = %d, want 404", res.StatusCode)
	}
	res, body := doReq(t, srv, http.MethodGet,
		"/v1/exclusions/preview?metric=body_fat_percentage", "", otherCookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var previewed int64
	if err := json.Unmarshal(body["measurements"], &previewed); err != nil {
		t.Fatalf("decode measurements: %v", err)
	}
	if previewed != 2 {
		t.Errorf("other Account's preview = %d, want its own 2 rows", previewed)
	}
}
