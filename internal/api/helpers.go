package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// envelope wraps every JSON response in a top-level object, so the payload is
// always self-describing ({"metrics": …}, {"error": …}) rather than a bare
// array or scalar — room to add metadata later without breaking clients.
type envelope map[string]any

// writeJSON serializes data as JSON, applies any extra headers, and writes it
// with the given status. A trailing newline makes the output pleasant in a
// terminal.
func writeJSON(w http.ResponseWriter, status int, data any, headers http.Header) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	for key, values := range headers {
		w.Header()[key] = values
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

// readJSON decodes a single JSON object from the request body into dst,
// rejecting unknown fields and trailing data. It is the write-path counterpart
// to writeJSON, used by the mutating endpoints (accounts, dashboards) that land
// in later slices; the read-only endpoints in this slice take no body.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("api: decode body: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("api: body must contain a single JSON object")
	}
	return nil
}
