package httpapi

import (
	"cmp"
	"encoding/json"
	"net/http"
)

// Problem is an RFC 9457 problem details body. Code is the stable machine-readable error code
// that the frontend translates.
type Problem struct {
	// Type defaults to about:blank.
	Type string `json:"type"`
	// Title defaults to the status text.
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

// WriteProblem answers p.Status with p as an application/problem+json body.
func WriteProblem(w http.ResponseWriter, p Problem) {
	p.Type = cmp.Or(p.Type, "about:blank")
	p.Title = cmp.Or(p.Title, http.StatusText(p.Status))

	// Marshaling strings and an int cannot fail.
	body, _ := json.Marshal(p)

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_, _ = w.Write(body)
}
