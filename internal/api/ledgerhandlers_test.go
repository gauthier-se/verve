package api

import (
	"net/http"
	"testing"
)

// The Ledger's folds are tested in internal/query, where they live. What is left
// here is that the route is behind a session.

func TestLedgerRequiresAuth(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res, _ := do(t, srv, "/v1/ledger")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}
