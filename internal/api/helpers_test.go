package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// readJSON is the write-path helper the issue mandates for the mutating
// endpoints in later slices; these tests lock its contract now.
func TestReadJSON(t *testing.T) {
	type payload struct {
		Email string `json:"email"`
	}
	newReq := func(body string) (*payload, error) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		var p payload
		return &p, readJSON(httptest.NewRecorder(), r, &p)
	}

	t.Run("single object decodes", func(t *testing.T) {
		p, err := newReq(`{"email":"a@b.c"}`)
		if err != nil || p.Email != "a@b.c" {
			t.Fatalf("got %+v, err %v; want email a@b.c", p, err)
		}
	})
	t.Run("unknown field rejected", func(t *testing.T) {
		if _, err := newReq(`{"email":"a@b.c","extra":1}`); err == nil {
			t.Fatal("expected error for unknown field")
		}
	})
	t.Run("trailing data rejected", func(t *testing.T) {
		if _, err := newReq(`{"email":"a@b.c"}{"email":"x"}`); err == nil {
			t.Fatal("expected error for a second JSON object")
		}
	})
	t.Run("malformed rejected", func(t *testing.T) {
		if _, err := newReq(`{not json`); err == nil {
			t.Fatal("expected error for malformed body")
		}
	})
}
