package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// listAnnotations returns the Account's Annotations for a query string, which is
// either a time axis (folded onto its bucket grid) or empty (the whole history).
func listAnnotations(t *testing.T, srv *Server, cookie *http.Cookie, qs string) []annotationView {
	t.Helper()
	res, body := doReq(t, srv, http.MethodGet, "/v1/annotations"+qs, "", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list annotations%s status = %d, want 200 (%s)", qs, res.StatusCode, body["error"])
	}
	var annotations []annotationView
	if err := json.Unmarshal(body["annotations"], &annotations); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	return annotations
}

// annotate creates one Annotation and returns it, failing the test if the write
// did not succeed.
func annotate(t *testing.T, srv *Server, cookie *http.Cookie, payload string) annotationView {
	t.Helper()
	res, body := doReq(t, srv, http.MethodPost, "/v1/annotations", payload, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create annotation status = %d, want 201 (%s)", res.StatusCode, body["error"])
	}
	var a annotationView
	if err := json.Unmarshal(body["annotation"], &a); err != nil {
		t.Fatalf("decode annotation: %v", err)
	}
	return a
}

// fieldErrors decodes the field-keyed 422 body every handler answers with.
func fieldErrors(t *testing.T, body map[string]json.RawMessage) map[string]string {
	t.Helper()
	var fields map[string]string
	if err := json.Unmarshal(body["error"], &fields); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return fields
}

// marchDay is a one-month custom window at the day bucket, the shape every
// folding assertion below is read against.
const marchDay = "?range_preset=custom&range_from=2026-03-01&range_to=2026-04-01&bucket=day"

func TestAnnotationsRequireAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, "/v1/annotations", ""},
		{http.MethodPost, "/v1/annotations", `{"label":"flu","starts_on":"2026-03-12"}`},
		{http.MethodPatch, "/v1/annotations/1", `{"label":"flu"}`},
		{http.MethodDelete, "/v1/annotations/1", ""},
	} {
		res, _ := doReq(t, srv, tc.method, tc.target, tc.body)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s %s status = %d, want 401", tc.method, tc.target, res.StatusCode)
		}
	}
}

func TestCreateAndListAnnotations(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	if got := listAnnotations(t, srv, cookie, ""); len(got) != 0 {
		t.Fatalf("new account has %d annotations, want 0", len(got))
	}

	created := annotate(t, srv, cookie,
		`{"label":"flu","body":"a week in bed","starts_on":"2026-03-12","ends_on":"2026-03-19"}`)
	if created.ID == 0 {
		t.Error("created annotation has no id")
	}
	if created.Body == nil || *created.Body != "a week in bed" {
		t.Errorf("body = %v, want %q", created.Body, "a week in bed")
	}

	// Without a time axis the list is the whole history and carries no buckets:
	// no axis, no folding.
	all := listAnnotations(t, srv, cookie, "")
	if len(all) != 1 {
		t.Fatalf("got %d annotations, want 1", len(all))
	}
	if all[0].Bucket != "" || all[0].EndBucket != "" {
		t.Errorf("unfolded annotation carries buckets %q/%q, want neither", all[0].Bucket, all[0].EndBucket)
	}
	if all[0].StartsOn != "2026-03-12" || all[0].EndsOn == nil || *all[0].EndsOn != "2026-03-19" {
		t.Errorf("span = %s..%v, want 2026-03-12..2026-03-19", all[0].StartsOn, all[0].EndsOn)
	}
}

func TestListAnnotationsFoldsOntoTheBucketGrid(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	annotate(t, srv, cookie, `{"label":"flu","starts_on":"2026-03-12","ends_on":"2026-03-19"}`)

	tests := []struct {
		name, bucket, wantStart, wantEnd string
	}{
		// At the day grain the span is eight buckets wide, so it draws as a band.
		{"day", "day", "2026-03-12", "2026-03-19"},
		// The 12th is a Thursday and the 19th the next, so two ISO weeks.
		{"week", "week", "2026-03-09", "2026-03-16"},
		// One month bucket holds the whole fortnight: one marker, no band, and
		// end_bucket is therefore absent rather than equal to bucket.
		{"month", "month", "2026-03-01", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := listAnnotations(t, srv, cookie,
				"?range_preset=custom&range_from=2026-03-01&range_to=2026-04-01&bucket="+tc.bucket)
			if len(got) != 1 {
				t.Fatalf("got %d annotations, want 1", len(got))
			}
			if got[0].Bucket != tc.wantStart || got[0].EndBucket != tc.wantEnd {
				t.Errorf("folded to (%q, %q), want (%q, %q)",
					got[0].Bucket, got[0].EndBucket, tc.wantStart, tc.wantEnd)
			}
			// The real dates travel alongside the grid positions: the tooltip says
			// "12 March", never "1 March", whatever the bucket.
			if got[0].StartsOn != "2026-03-12" {
				t.Errorf("starts_on = %q, want 2026-03-12 (the folded bucket must not overwrite it)", got[0].StartsOn)
			}
		})
	}
}

// TestListAnnotationsOverlappingTheWindow is the case a filter on starts_on alone
// silently drops: a span that began before the range and is still running is on
// screen for the days it covers, and has to be drawn there.
func TestListAnnotationsOverlappingTheWindow(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	annotate(t, srv, cookie, `{"label":"before","starts_on":"2026-02-20","ends_on":"2026-03-03"}`)
	annotate(t, srv, cookie, `{"label":"after","starts_on":"2026-03-28","ends_on":"2026-04-15"}`)
	annotate(t, srv, cookie, `{"label":"inside","starts_on":"2026-03-12"}`)
	annotate(t, srv, cookie, `{"label":"long past","starts_on":"2025-01-01","ends_on":"2025-01-05"}`)
	annotate(t, srv, cookie, `{"label":"long future","starts_on":"2027-01-01"}`)

	got := listAnnotations(t, srv, cookie, marchDay)
	labels := make([]string, 0, len(got))
	for _, a := range got {
		labels = append(labels, a.Label)
	}
	want := []string{"before", "inside", "after"} // chronological by start day
	if len(labels) != len(want) {
		t.Fatalf("got labels %v, want %v", labels, want)
	}
	for i, label := range want {
		if labels[i] != label {
			t.Errorf("annotation %d = %q, want %q", i, labels[i], label)
		}
	}

	// Both overhanging spans are clamped to the window's own bucket grid: a marker
	// outside the drawn range would be a category the chart does not have.
	if got[0].Bucket != "2026-03-01" || got[0].EndBucket != "2026-03-03" {
		t.Errorf("span starting before the window folded to (%q, %q), want (2026-03-01, 2026-03-03)",
			got[0].Bucket, got[0].EndBucket)
	}
	if got[2].Bucket != "2026-03-28" || got[2].EndBucket != "2026-03-31" {
		t.Errorf("span running past the window folded to (%q, %q), want (2026-03-28, 2026-03-31)",
			got[2].Bucket, got[2].EndBucket)
	}
	// A single day is a marker: no end bucket, and nothing for a band to span.
	if got[1].Bucket != "2026-03-12" || got[1].EndBucket != "" {
		t.Errorf("single day folded to (%q, %q), want (2026-03-12, \"\")", got[1].Bucket, got[1].EndBucket)
	}
}

// TestListAnnotationsIgnoresTheBaseline: a Baseline series is drawn on the current
// window's ordinal axis, so a marker over it would sit at a bucket whose date is
// not the date under it. Markers describe the current range only (ADR 0030).
func TestListAnnotationsIgnoresTheBaseline(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	annotate(t, srv, cookie, `{"label":"flu","starts_on":"2026-03-12"}`)

	plain := listAnnotations(t, srv, cookie, marchDay)
	compared := listAnnotations(t, srv, cookie, marchDay+"&baseline_rule=previous")
	if len(compared) != len(plain) {
		t.Fatalf("with a baseline got %d annotations, want %d", len(compared), len(plain))
	}
	if compared[0].Bucket != plain[0].Bucket {
		t.Errorf("baseline moved the marker to %q, want %q", compared[0].Bucket, plain[0].Bucket)
	}
}

func TestListAnnotationsRejectsABrokenTimeAxis(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := doReq(t, srv, http.MethodGet, "/v1/annotations?range_preset=fortnight", "", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	if _, ok := fieldErrors(t, body)["range_preset"]; !ok {
		t.Errorf("error = %v, want a range_preset field error", fieldErrors(t, body))
	}
}

func TestUpdateAnnotation(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	created := annotate(t, srv, cookie,
		`{"label":"flu","body":"a week in bed","starts_on":"2026-03-12","ends_on":"2026-03-19"}`)

	res, body := doReq(t, srv, http.MethodPatch, "/v1/annotations/"+itoa(created.ID),
		`{"label":"flu, then a cold"}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var patched annotationView
	if err := json.Unmarshal(body["annotation"], &patched); err != nil {
		t.Fatalf("decode annotation: %v", err)
	}
	if patched.Label != "flu, then a cold" {
		t.Errorf("label = %q, want %q", patched.Label, "flu, then a cold")
	}
	// An omitted field is unchanged: a label edit must not silently drop the span.
	if patched.EndsOn == nil || *patched.EndsOn != "2026-03-19" {
		t.Errorf("ends_on = %v, want 2026-03-19 unchanged", patched.EndsOn)
	}
	if patched.Body == nil || *patched.Body != "a week in bed" {
		t.Errorf("body = %v, want unchanged", patched.Body)
	}
}

// TestUpdateAnnotationClearsASpan: an empty string is how a span becomes a single
// day again, and how a body is emptied, the shape an emptied form field sends.
// Absent means "leave it alone", which is what the pointer inputs are for.
func TestUpdateAnnotationClearsASpan(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	created := annotate(t, srv, cookie,
		`{"label":"trip","body":"Lisbon","starts_on":"2026-03-12","ends_on":"2026-03-19"}`)

	res, body := doReq(t, srv, http.MethodPatch, "/v1/annotations/"+itoa(created.ID),
		`{"ends_on":"","body":""}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	got := listAnnotations(t, srv, cookie, "")
	if len(got) != 1 {
		t.Fatalf("got %d annotations, want 1", len(got))
	}
	if got[0].EndsOn != nil {
		t.Errorf("ends_on = %v, want nil", *got[0].EndsOn)
	}
	if got[0].Body != nil {
		t.Errorf("body = %v, want nil", *got[0].Body)
	}
}

func TestDeleteAnnotation(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	created := annotate(t, srv, cookie, `{"label":"flu","starts_on":"2026-03-12"}`)

	res, _ := doReq(t, srv, http.MethodDelete, "/v1/annotations/"+itoa(created.ID), "", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", res.StatusCode)
	}
	if got := listAnnotations(t, srv, cookie, ""); len(got) != 0 {
		t.Errorf("got %d annotations after delete, want 0", len(got))
	}
	// Deleting it again is a 404, not a 204: unlike a Pin, an Annotation has an id
	// rather than a natural key, so "that row" either exists or it does not.
	res, _ = doReq(t, srv, http.MethodDelete, "/v1/annotations/"+itoa(created.ID), "", cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", res.StatusCode)
	}
}

func TestAnnotationValidation(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	tests := []struct {
		name, payload, field string
	}{
		{"no label", `{"starts_on":"2026-03-12"}`, "label"},
		{"blank label", `{"label":"   ","starts_on":"2026-03-12"}`, "label"},
		{"label too long", `{"label":"` + repeat("a", maxLabelLen+1) + `","starts_on":"2026-03-12"}`, "label"},
		{"body too long", `{"label":"flu","body":"` + repeat("a", maxBodyLen+1) + `","starts_on":"2026-03-12"}`, "body"},
		{"no start day", `{"label":"flu"}`, "starts_on"},
		{"start day is not a day", `{"label":"flu","starts_on":"2026-03-12T08:00:00Z"}`, "starts_on"},
		{"end day is not a day", `{"label":"flu","starts_on":"2026-03-12","ends_on":"nope"}`, "ends_on"},
		{"inverted span", `{"label":"flu","starts_on":"2026-03-19","ends_on":"2026-03-12"}`, "ends_on"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, body := doReq(t, srv, http.MethodPost, "/v1/annotations", tc.payload, cookie)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", res.StatusCode)
			}
			if _, ok := fieldErrors(t, body)[tc.field]; !ok {
				t.Errorf("error = %v, want a %s field error", fieldErrors(t, body), tc.field)
			}
		})
	}
}

// TestAnnotationsAreAccountScoped: one Account can neither see, edit nor delete
// another's notes, and a foreign id is a 404 rather than a 403 so a probe cannot
// tell missing from forbidden (ADR 0007).
func TestAnnotationsAreAccountScoped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	mine := annotate(t, srv, cookie, `{"label":"flu","starts_on":"2026-03-12"}`)

	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	otherCookie := login(t, srv, "other@example.com", testPassword)

	if got := listAnnotations(t, srv, otherCookie, ""); len(got) != 0 {
		t.Errorf("other account sees %d annotations, want 0", len(got))
	}
	if got := listAnnotations(t, srv, otherCookie, marchDay); len(got) != 0 {
		t.Errorf("other account sees %d annotations in the window, want 0", len(got))
	}

	res, _ := doReq(t, srv, http.MethodPatch, "/v1/annotations/"+itoa(mine.ID), `{"label":"theirs"}`, otherCookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account patch status = %d, want 404", res.StatusCode)
	}
	res, _ = doReq(t, srv, http.MethodDelete, "/v1/annotations/"+itoa(mine.ID), "", otherCookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account delete status = %d, want 404", res.StatusCode)
	}

	if got := listAnnotations(t, srv, cookie, ""); len(got) != 1 {
		t.Errorf("owner now has %d annotations, want 1", len(got))
	}
}

// repeat builds an over-long field value for the bound checks.
func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

// TestCreateAnnotationAcceptsEmptyOptionalFields: the dialog always sends every
// field, so an untouched "To" and an untouched body arrive as empty strings. They
// must mean "no span" and "no body" rather than a 422 on a form nobody filled in
// wrong.
func TestCreateAnnotationAcceptsEmptyOptionalFields(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	created := annotate(t, srv, cookie, `{"label":"flu","body":"","starts_on":"2026-03-12","ends_on":""}`)
	if created.EndsOn != nil {
		t.Errorf("ends_on = %v, want nil for an empty string", *created.EndsOn)
	}
	if created.Body != nil {
		t.Errorf("body = %v, want nil for an empty string", *created.Body)
	}
	// A blank label is still a blank label: the one required field stays required.
	res, _ := doReq(t, srv, http.MethodPost, "/v1/annotations",
		`{"label":"","body":"","starts_on":"2026-03-12","ends_on":""}`, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("blank label status = %d, want 422", res.StatusCode)
	}
}
