package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
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

// valueOrEmpty renders a nullable text column as a string, an absent value being
// the empty one for a JSON field that omits it.
func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// resolveAxis resolves a Dashboard's time axis from request tokens, folding a
// validation failure into v rather than answering with it.
//
// Folding is the point. A request usually carries its own checks beside the axis
// (a metric slug, a lag, an activity filter), and answering on the first failure
// would make a client fix one field per round trip. Every caller wrote the same
// type switch to get that, and the one that did not (the Annotations list) could
// report a bad range or a bad metric but never both.
//
// ok is false only for a genuine fault, and the response is already written. A
// validation failure returns true with v holding the fields, because the caller
// has its own checks to add before it answers.
func (s *Server) resolveAxis(w http.ResponseWriter, r *http.Request, v *Validator, t timeaxis.Tokens) (timeaxis.Resolved, bool) {
	resolved, err := timeaxis.Resolve(t, time.Now())
	if inv, ok := err.(timeaxis.Invalid); ok {
		mergeInvalid(v, inv)
		return resolved, true
	}
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return resolved, false
	}
	return resolved, true
}

// mergeInvalid folds a timeaxis validation failure into v. It is separate from
// resolveAxis for the one caller that validates stored tokens without resolving
// them: a Dashboard being saved has no clock to resolve against.
func mergeInvalid(v *Validator, err error) {
	inv, ok := err.(timeaxis.Invalid)
	if !ok {
		return
	}
	for field, msg := range inv {
		v.AddError(field, msg)
	}
}
