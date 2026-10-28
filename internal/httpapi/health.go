package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
)

// Checker is a readiness check of /readyz.
type Checker interface {
	// Name prefixes the reason of a failure, for example "database".
	Name() string
	// Check returns nil when the checked dependency is ready, otherwise why it is not.
	Check(ctx context.Context) error
}

// readyzResponse is the body of /readyz.
type readyzResponse struct {
	Status  string   `json:"status"`
	Version string   `json:"version"`
	Reasons []string `json:"reasons,omitempty"`
}

// IsHealthProbe reports whether path is one of the health probes, /healthz and /readyz.
func IsHealthProbe(path string) bool {
	return slices.Contains(probePaths, path)
}

// healthz answers 200 as long as the server responds.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz answers 200 when every check returned by checkers passes, otherwise 503 with one reason
// per failing check. Both carry the version.
func readyz(version string, checkers func() []Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reasons []string

		if checkers != nil {
			for _, c := range checkers() {
				if err := c.Check(r.Context()); err != nil {
					reasons = append(reasons, c.Name()+": "+err.Error())
				}
			}
		}

		if len(reasons) > 0 {
			writeJSON(w, http.StatusServiceUnavailable, readyzResponse{Status: "not_ready", Version: version, Reasons: reasons})

			return
		}

		writeJSON(w, http.StatusOK, readyzResponse{Status: "ready", Version: version})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
