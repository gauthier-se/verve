package httpapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Problem is an RFC 9457 problem details body. Code is the stable machine-readable error code
// that the frontend translates. It is also the error type of the API operations: Huma answers
// with its status as application/problem+json.
type Problem struct {
	// Type defaults to about:blank.
	Type string `json:"type" doc:"A URI reference that identifies the problem type, about:blank when only the status matters."`
	// Title defaults to the status text.
	Title    string `json:"title" doc:"A short summary of the problem type."`
	Status   int    `json:"status" doc:"The HTTP status code."`
	Detail   string `json:"detail,omitempty" doc:"An explanation specific to this occurrence of the problem."`
	Instance string `json:"instance,omitempty" doc:"A URI reference that identifies this occurrence of the problem."`
	Code     string `json:"code" doc:"A stable machine-readable error code."`
	// Errors are the individual errors behind the problem, for example one per invalid field. Left
	// out when empty, never null.
	Errors []*huma.ErrorDetail `json:"errors,omitempty" nullable:"false" doc:"The individual errors behind the problem, for example one per invalid field."`
}

// Error returns the detail of the problem, or its title when it has none.
func (p *Problem) Error() string {
	return cmp.Or(p.Detail, p.Title)
}

// GetStatus returns the status that Huma answers with.
func (p *Problem) GetStatus() int {
	return p.Status
}

// ContentType makes Huma send the problem as application/problem+json.
func (p *Problem) ContentType(string) string {
	return "application/problem+json"
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

// newHumaError replaces huma.NewError, so that the errors of Huma and of the operations, such as
// validation errors, are problems. Its code defaults from the status, and errs become its details.
func newHumaError(status int, detail string, errs ...error) huma.StatusError {
	p := &Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   defaultCode(status),
	}

	for _, err := range errs {
		if err == nil {
			continue
		}

		var detailer huma.ErrorDetailer
		if errors.As(err, &detailer) {
			p.Errors = append(p.Errors, detailer.ErrorDetail())

			continue
		}

		p.Errors = append(p.Errors, &huma.ErrorDetail{Message: err.Error()})
	}

	return p
}

// defaultCode returns the code of a problem that only its status describes. The other client
// errors are bad_request and the other server errors internal, so that the frontend knows every
// code.
func defaultCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "validation_failed"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	}

	if status >= http.StatusInternalServerError {
		return "internal"
	}

	return "bad_request"
}
