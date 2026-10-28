package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/version"
)

// apiHandler returns the handler of a server built as version 1.2.3.
func apiHandler() http.Handler {
	return NewHandler(Deps{
		Logger:  slog.New(slog.DiscardHandler),
		Clock:   clock.Real(),
		Version: version.Info{Version: "1.2.3", Commit: "abc1234", BuildDate: "2026-10-28T10:00:00Z"},
	})
}

// Given GET /api/v1/meta without authentication, then 200 with the name of the application and the
// build metadata.
func TestAPI_GetMetaReturnsBuild(t *testing.T) {
	t.Parallel()

	rec := serve(t, apiHandler(), http.MethodGet, "/api/v1/meta")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"name":"Verve","version":"1.2.3","commit":"abc1234","buildDate":"2026-10-28T10:00:00Z"}`,
		rec.Body.String())
}

// openAPISpec holds the parts of an OpenAPI document that the tests check.
type openAPISpec struct {
	OpenAPI string `json:"openapi"`
	Paths   map[string]map[string]struct {
		OperationID string `json:"operationId"`
		Responses   map[string]struct {
			Content map[string]struct {
				Schema struct {
					Ref string `json:"$ref"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"responses"`
	} `json:"paths"`
}

// Given GET /api/openapi.json, then an OpenAPI 3.1 document with the operation get-meta, whose
// errors are problems. Given GET /api/openapi.yaml, then the same document in YAML.
func TestAPI_ServesOpenAPISpec(t *testing.T) {
	t.Parallel()

	h := apiHandler()

	rec := serve(t, h, http.MethodGet, "/api/openapi.json")
	require.Equal(t, http.StatusOK, rec.Code)

	var spec openAPISpec
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &spec))
	require.True(t, strings.HasPrefix(spec.OpenAPI, "3.1"), spec.OpenAPI)

	getMeta := spec.Paths["/api/v1/meta"]["get"]
	require.Equal(t, "get-meta", getMeta.OperationID)
	require.Equal(t, "#/components/schemas/Problem",
		getMeta.Responses["default"].Content["application/problem+json"].Schema.Ref)

	yamlRec := serve(t, h, http.MethodGet, "/api/openapi.yaml")
	require.Equal(t, http.StatusOK, yamlRec.Code)

	var fromYAML any
	require.NoError(t, yaml.Unmarshal(yamlRec.Body.Bytes(), &fromYAML))

	yamlAsJSON, err := json.Marshal(fromYAML)
	require.NoError(t, err)
	require.JSONEq(t, rec.Body.String(), string(yamlAsJSON))
}

// Given GET /api/docs, then a 404 problem: the documentation page is deferred to M4, so that no
// third-party asset is loaded. Given a schema path, then a 404 problem: the schemas are only in the
// spec. Given another unknown path under /api/, then the 404 problem still answers it next to the
// API operations.
func TestAPI_UnknownPathsReturnProblem(t *testing.T) {
	t.Parallel()

	h := apiHandler()

	for _, tt := range []struct{ method, target string }{
		{method: http.MethodGet, target: "/api/docs"},
		{method: http.MethodGet, target: "/api/schemas/Meta.json"},
		{method: http.MethodGet, target: "/api/schemas/Nope.json"},
		{method: http.MethodGet, target: "/api/v1/unknown"},
		{method: http.MethodGet, target: "/api/v1/meta/unknown"},
		{method: http.MethodPost, target: "/api/v1/unknown"},
	} {
		rec := serve(t, h, tt.method, tt.target)

		require.Equal(t, http.StatusNotFound, rec.Code, tt.target)
		require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"), tt.target)
		require.JSONEq(t, `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found"}`,
			rec.Body.String(), tt.target)
	}
}

// testValidationInput is the input of a test-only operation with validation rules.
type testValidationInput struct {
	Limit int `query:"limit" minimum:"1"`
	Body  struct {
		Name string `json:"name" minLength:"3"`
	}
}

// Given a request that fails the validation of an operation, then 422 with a problem whose code is
// validation_failed and whose errors are Huma's details.
func TestAPI_ValidationErrorReturnsProblem(t *testing.T) {
	t.Parallel()

	api := NewAPI(http.NewServeMux(), APIDeps{})
	huma.Register(api, huma.Operation{
		OperationID: "test-validation",
		Method:      http.MethodPost,
		Path:        "/api/v1/test/validation",
	}, func(context.Context, *testValidationInput) (*struct{}, error) {
		return nil, nil
	})

	rec := humatest.Wrap(t, api).Post("/api/v1/test/validation?limit=0", map[string]any{"name": "ab"})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{
		"type": "about:blank",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"code": "validation_failed",
		"errors": [
			{"message": "expected number >= 1", "location": "query.limit", "value": 0},
			{"message": "expected length >= 3", "location": "body.name", "value": "ab"}
		]
	}`, rec.Body.String())
}

// Given an operation failing with an error that is not a problem, then 500 with the internal code
// and without the message of the error, which may hold internal details such as file paths.
func TestAPI_UnexpectedErrorHidesItsMessage(t *testing.T) {
	t.Parallel()

	api := NewAPI(http.NewServeMux(), APIDeps{})
	huma.Register(api, huma.Operation{
		OperationID: "test-unexpected-error",
		Method:      http.MethodGet,
		Path:        "/api/v1/test/unexpected-error",
	}, func(context.Context, *struct{}) (*struct{}, error) {
		return nil, errors.New("open /data/verve.db: permission denied")
	})

	rec := humatest.Wrap(t, api).Get("/api/v1/test/unexpected-error")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"type":"about:blank","title":"Internal Server Error","status":500,`+
		`"detail":"unexpected error occurred","code":"internal"}`, rec.Body.String())
}

// testErrorInput is the input of a test-only operation that fails with the requested status.
type testErrorInput struct {
	Status int `path:"status"`
}

// Given an operation failing with a Huma error, then the problem code defaults from the status:
// the known statuses have their own code, the other client errors are bad_request and the other
// server errors internal.
func TestAPI_ErrorCodeDefaultsFromStatus(t *testing.T) {
	t.Parallel()

	api := NewAPI(http.NewServeMux(), APIDeps{})
	huma.Register(api, huma.Operation{
		OperationID: "test-error",
		Method:      http.MethodGet,
		Path:        "/api/v1/test/errors/{status}",
	}, func(_ context.Context, in *testErrorInput) (*struct{}, error) {
		return nil, huma.NewError(in.Status, "failed on purpose")
	})

	for _, tt := range []struct {
		status int
		code   string
	}{
		{status: http.StatusBadRequest, code: "bad_request"},
		{status: http.StatusUnauthorized, code: "unauthorized"},
		{status: http.StatusForbidden, code: "forbidden"},
		{status: http.StatusNotFound, code: "not_found"},
		{status: http.StatusConflict, code: "conflict"},
		{status: http.StatusUnprocessableEntity, code: "validation_failed"},
		{status: http.StatusTooManyRequests, code: "rate_limited"},
		{status: http.StatusInternalServerError, code: "internal"},
		{status: http.StatusServiceUnavailable, code: "unavailable"},
		{status: http.StatusRequestEntityTooLarge, code: "bad_request"},
		{status: http.StatusBadGateway, code: "internal"},
	} {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			t.Parallel()

			rec := humatest.Wrap(t, api).Get(fmt.Sprintf("/api/v1/test/errors/%d", tt.status))

			require.Equal(t, tt.status, rec.Code)
			require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			require.JSONEq(t, fmt.Sprintf(`{"type":"about:blank","title":%q,"status":%d,"detail":"failed on purpose","code":%q}`,
				http.StatusText(tt.status), tt.status, tt.code), rec.Body.String())
		})
	}
}
