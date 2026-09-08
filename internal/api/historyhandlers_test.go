package api

import (
	"net/http"
	"testing"
)

// The History page's behaviour is tested in internal/history and internal/query,
// through the interfaces that own it. What is left here is what only this layer
// can answer: that the route is behind a session, and that a Metric outside the
// Catalog is a validation error rather than an empty page.

func TestHistoryRequiresAuth(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res, _ := do(t, srv, "/v1/history")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}

func TestHistoryRejectsUnknownMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := do(t, srv, "/v1/history?metric=not_a_metric", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown metric status = %d, want 422", res.StatusCode)
	}
}
