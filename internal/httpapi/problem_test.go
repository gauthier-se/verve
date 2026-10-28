package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Given a problem, then WriteProblem answers its status with an RFC 9457 body and the
// application/problem+json content type, leaving out the empty optional fields.
func TestWriteProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		problem Problem
		want    string
	}{
		{
			name:    "given only the status and the code, then the type is about:blank and the title the status text",
			problem: Problem{Status: http.StatusNotFound, Code: "not_found"},
			want:    `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found"}`,
		},
		{
			name: "given every field, then they are all written",
			problem: Problem{
				Type:     "https://verve.example/problems/conflict",
				Title:    "Already imported",
				Status:   http.StatusConflict,
				Detail:   "This file was imported on 2026-10-01.",
				Instance: "/api/v1/imports/42",
				Code:     "import_duplicate",
			},
			want: `{"type":"https://verve.example/problems/conflict","title":"Already imported","status":409,` +
				`"detail":"This file was imported on 2026-10-01.","instance":"/api/v1/imports/42","code":"import_duplicate"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			WriteProblem(rec, tt.problem)

			require.Equal(t, tt.problem.Status, rec.Code)
			require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			require.Equal(t, tt.want, rec.Body.String())
		})
	}
}
