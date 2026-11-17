package httpapi

import (
	"net/http"
	"reflect"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

// describeUploads adds the operations of the tus endpoint to the spec of api by hand, since the
// endpoint is a plain net/http handler (ADR 0004, ADR 0015).
func describeUploads(api huma.API) {
	oapi := api.OpenAPI()
	problem := oapi.Components.Schemas.Schema(reflect.TypeFor[Problem](), true, "")

	tusResumable := headerParam("Tus-Resumable", "The version of the tus protocol, `1.0.0`.", true)
	uploadID := &huma.Param{
		Name:        "id",
		In:          "path",
		Description: "The id of the upload, the last segment of its Location.",
		Required:    true,
		Schema:      &huma.Schema{Type: huma.TypeString},
	}

	add := func(op *huma.Operation, success int, headers map[string]string, errors ...int) {
		op.Tags = []string{"Ingestion"}
		op.Security = sessionSecurity()
		op.Responses = map[string]*huma.Response{
			strconv.Itoa(success): {
				Description: http.StatusText(success),
				Headers:     responseHeaders(headers),
			},
		}

		for _, status := range errors {
			response := &huma.Response{Description: http.StatusText(status)}
			if op.Method != http.MethodHead {
				response.Content = map[string]*huma.MediaType{"application/problem+json": {Schema: problem}}
			}

			op.Responses[strconv.Itoa(status)] = response
		}

		oapi.AddOperation(op)
	}

	const (
		resumable = "The version of the tus protocol that the server speaks, `1.0.0`."
		expires   = "When the upload expires, 24 hours after its last write, in the HTTP date format."
	)

	add(&huma.Operation{
		OperationID: "describe-uploads",
		Method:      http.MethodOptions,
		Path:        uploadsPathPrefix,
		Summary:     "Describe the tus server of the uploads",
		Description: "Answers the version, the extensions and the largest upload of the tus server that " +
			"receives the exports of Apple Health, `VERVE_MAX_UPLOAD_SIZE`. Accepts a session only.",
	}, http.StatusNoContent, map[string]string{
		"Tus-Resumable": resumable,
		"Tus-Version":   "The versions of the tus protocol that the server serves, `1.0.0`.",
		"Tus-Extension": "The extensions of the tus protocol that the server serves, `creation,termination,expiration`.",
		"Tus-Max-Size":  "The largest upload in bytes, `VERVE_MAX_UPLOAD_SIZE`.",
	}, http.StatusUnauthorized)

	add(&huma.Operation{
		OperationID: "create-upload",
		Method:      http.MethodPost,
		Path:        uploadsPathPrefix,
		Summary:     "Create a resumable upload of an export",
		Description: "Creates a resumable upload of `Upload-Length` bytes with the tus protocol 1.0.0, for an " +
			"`export.zip` or an `export.xml` of Apple Health. The body is sent with `PATCH` on its `Location`. " +
			"A length above `VERVE_MAX_UPLOAD_SIZE` answers 413, and `Upload-Defer-Length` is not supported. " +
			"An upload expires 24 hours after its last write. Called through `tus-js-client`. Accepts a " +
			"session only.",
		Parameters: []*huma.Param{
			tusResumable,
			headerParam("Upload-Length", "The size of the whole upload in bytes, at least 1.", true),
			headerParam("Upload-Metadata", "The metadata of the upload, as keys with base64 values separated "+
				"by commas. Only `filename` is kept, for display.", false),
		},
	}, http.StatusCreated, map[string]string{
		"Tus-Resumable":  resumable,
		"Location":       "The URL of the upload.",
		"Upload-Expires": expires,
	}, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusPreconditionFailed,
		http.StatusRequestEntityTooLarge, http.StatusInsufficientStorage)

	add(&huma.Operation{
		OperationID: "get-upload-offset",
		Method:      http.MethodHead,
		Path:        uploadsPathPrefix + "{id}",
		Summary:     "Get the offset of an upload",
		Description: "Answers the number of bytes of the upload that the server holds, from which the client " +
			"resumes it. An upload of another user, unknown, expired, or completed into an import answers 404. " +
			"Accepts a session only.",
		Parameters: []*huma.Param{uploadID, tusResumable},
	}, http.StatusOK, map[string]string{
		"Tus-Resumable":   resumable,
		"Upload-Offset":   "The number of bytes received.",
		"Upload-Length":   "The size of the whole upload in bytes.",
		"Upload-Expires":  expires,
		"Upload-Metadata": "The filename of the upload, when it has one.",
		"Cache-Control":   "`no-store`.",
	}, http.StatusUnauthorized, http.StatusNotFound, http.StatusPreconditionFailed)

	add(&huma.Operation{
		OperationID: "write-upload",
		Method:      http.MethodPatch,
		Path:        uploadsPathPrefix + "{id}",
		Summary:     "Write the next bytes of an upload",
		Description: "Writes the body at `Upload-Offset`, the offset of the upload, and answers the new offset " +
			"once the bytes are synced. Another offset answers 409, a body past `Upload-Length` 400, and a " +
			"write while another runs on the upload 423. A full disk answers 507: resume from the offset of " +
			"`HEAD` once space is freed. The write that reaches the length completes the upload into a queued " +
			"XML import, whose id is in `Verve-Import-Id`, and the upload is then gone. Accepts a session only.",
		Parameters: []*huma.Param{
			uploadID, tusResumable,
			headerParam("Upload-Offset", "The offset of the upload, where the body starts.", true),
		},
		RequestBody: &huma.RequestBody{
			Description: "The next bytes of the upload.",
			Required:    true,
			Content:     map[string]*huma.MediaType{"application/offset+octet-stream": {}},
		},
	}, http.StatusNoContent, map[string]string{
		"Tus-Resumable":   resumable,
		"Upload-Offset":   "The new offset of the upload.",
		"Upload-Expires":  expires + " Left out once the upload is complete.",
		"Verve-Import-Id": "The id of the import of the completed upload, set by the write that reaches its length.",
	}, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnsupportedMediaType, http.StatusLocked,
		http.StatusInsufficientStorage)

	add(&huma.Operation{
		OperationID: "delete-upload",
		Method:      http.MethodDelete,
		Path:        uploadsPathPrefix + "{id}",
		Summary:     "Delete an upload",
		Description: "Removes an incomplete upload and its file. Accepts a session only.",
		Parameters:  []*huma.Param{uploadID, tusResumable},
	}, http.StatusNoContent, map[string]string{
		"Tus-Resumable": resumable,
	}, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusPreconditionFailed,
		http.StatusLocked)
}

// headerParam returns the request header name described by description.
func headerParam(name, description string, required bool) *huma.Param {
	return &huma.Param{
		Name:        name,
		In:          "header",
		Description: description,
		Required:    required,
		Schema:      &huma.Schema{Type: huma.TypeString},
	}
}

// responseHeaders returns the response headers of descriptions, by their name.
func responseHeaders(descriptions map[string]string) map[string]*huma.Header {
	headers := make(map[string]*huma.Header, len(descriptions))
	for name, description := range descriptions {
		headers[name] = &huma.Header{Description: description, Schema: &huma.Schema{Type: huma.TypeString}}
	}

	return headers
}
