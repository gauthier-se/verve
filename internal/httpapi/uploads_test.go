package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// uploadsPath is the path of the tus endpoint.
const uploadsPath = "/api/v1/uploads/"

// uploadsTestMaxSize is the largest upload that the server of the upload tests accepts.
const uploadsTestMaxSize = 1000

// exportMetadata is the Upload-Metadata of an upload of export.zip, as tus-js-client sends it.
const exportMetadata = "filename ZXhwb3J0LnppcA==,filetype YXBwbGljYXRpb24vemlw"

// uploadsTest is a server whose uploads and inbox are temporary directories, with the users alice
// and bob.
type uploadsTest struct {
	ingestTest

	uploadsDir string
	bob        store.User
	bobCookie  string
}

// newUploadsTest returns an uploadsTest on a fresh store, at authTestNow.
func newUploadsTest(t *testing.T) uploadsTest {
	t.Helper()

	ut := uploadsTest{ingestTest: newIngestTest(t), uploadsDir: t.TempDir()}
	ut.bob = ut.mustCreateUser(t, "bob", store.RoleMember)
	ut.bobCookie = "verve_session=" + ut.mustCreateSession(t, ut.bob)

	return ut
}

// files returns the real files of the uploads, which complete into the real inbox.
func (ut uploadsTest) files() *ingest.Uploads {
	return ingest.NewUploads(ut.uploadsDir, ut.inbox(), ut.clock)
}

// handler returns the handler of the server, which keeps the files of the uploads in files.
func (ut uploadsTest) handler(t *testing.T, files UploadFiles) http.Handler {
	t.Helper()

	deps := ut.deps(t, "")
	deps.Inbox = ut.inbox()
	deps.Uploads = files
	deps.MaxUploadSize = uploadsTestMaxSize

	return NewHandler(deps)
}

// uploadFiles returns the names of the files of the uploads.
func (ut uploadsTest) uploadFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(ut.uploadsDir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// requireNoUpload requires that no upload is stored and that no file of an upload exists.
func (ut uploadsTest) requireNoUpload(t *testing.T) {
	t.Helper()

	ids, err := ut.store.Uploads().IDs(t.Context())
	require.NoError(t, err)
	require.Empty(t, ids)
	require.Empty(t, ut.uploadFiles(t))
}

// tusRequest returns a request of method on target with body, as tus-js-client sends it from the web
// app: with Tus-Resumable: 1.0.0, and with the session cookie, the CSRF header and the Origin when
// cookie is not empty.
func tusRequest(t *testing.T, method, target string, body io.Reader, cookie string) *http.Request {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, "http://example.com"+target, body)
	r.Header.Set("Tus-Resumable", "1.0.0")

	if cookie != "" {
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Verve-Csrf", "1")
		r.Header.Set("Origin", "http://example.com")
	}

	return r
}

// creationRequest returns the request of alice that creates an upload of length bytes with
// metadata, left out when empty.
func (ut uploadsTest) creationRequest(t *testing.T, length int64, metadata string) *http.Request {
	t.Helper()

	r := tusRequest(t, http.MethodPost, uploadsPath, nil, ut.aliceCookie)
	r.Header.Set("Upload-Length", strconv.FormatInt(length, 10))

	if metadata != "" {
		r.Header.Set("Upload-Metadata", metadata)
	}

	return r
}

// mustCreate creates an upload of alice of length bytes of export.zip on h, requires 201 and
// returns its location.
func (ut uploadsTest) mustCreate(t *testing.T, h http.Handler, length int64) string {
	t.Helper()

	rec := serveWith(h, ut.creationRequest(t, length, exportMetadata))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	return rec.Header().Get("Location")
}

// uploadID returns the id of the upload of location.
func uploadID(location string) string {
	return strings.TrimPrefix(location, uploadsPath)
}

// head serves HEAD on location on h with cookie and returns the response.
func head(t *testing.T, h http.Handler, location, cookie string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, tusRequest(t, http.MethodHead, location, nil, cookie))
}

// Given OPTIONS /api/v1/uploads/, then it answers 204 with the version, the extensions and the
// largest size of the tus server, the size being VERVE_MAX_UPLOAD_SIZE.
func TestUploads_Options_DescribesTheServer(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())

	r := tusRequest(t, http.MethodOptions, uploadsPath, nil, ut.aliceCookie)
	r.Header.Del("Tus-Resumable")

	rec := serveWith(h, r)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, "1.0.0", rec.Header().Get("Tus-Resumable"))
	require.Equal(t, "1.0.0", rec.Header().Get("Tus-Version"))
	require.Equal(t, "creation,termination,expiration", rec.Header().Get("Tus-Extension"))
	require.Equal(t, "1000", rec.Header().Get("Tus-Max-Size"))
}

// Given a session, when POST /api/v1/uploads/ sends an Upload-Length within the limit, then it
// answers 201 with the Location of the upload and its Upload-Expires, 24 hours later, and stores the
// upload with the filename of Upload-Metadata and its empty file.
func TestUploads_Create_StoresTheUpload(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())

	rec := serveWith(h, ut.creationRequest(t, uploadsTestMaxSize, exportMetadata))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "1.0.0", rec.Header().Get("Tus-Resumable"))
	require.Equal(t, "Thu, 05 Nov 2026 08:00:00 GMT", rec.Header().Get("Upload-Expires"))

	location := rec.Header().Get("Location")
	require.True(t, strings.HasPrefix(location, uploadsPath), location)

	upload, err := ut.store.Uploads().Get(t.Context(), ut.alice.ID, uploadID(location))
	require.NoError(t, err)
	require.Equal(t, store.Upload{
		ID:        uploadID(location),
		UserID:    ut.alice.ID,
		FileName:  "export.zip",
		Length:    uploadsTestMaxSize,
		CreatedAt: authTestNow,
		ExpiresAt: authTestNow.Add(24 * time.Hour),
	}, upload)

	require.Equal(t, []string{upload.ID}, ut.uploadFiles(t))
	require.FileExists(t, ut.uploadsDir+"/"+upload.ID)

	info, err := os.Stat(ut.uploadsDir + "/" + upload.ID)
	require.NoError(t, err)
	require.Zero(t, info.Size())
}

// Given an upload created without Upload-Metadata or without its filename, then it has no file
// name.
func TestUploads_Create_WithoutFileName(t *testing.T) {
	t.Parallel()

	for _, metadata := range []string{"", "filetype YXBwbGljYXRpb24vemlw", "filename"} {
		t.Run(metadata, func(t *testing.T) {
			t.Parallel()

			ut := newUploadsTest(t)
			h := ut.handler(t, ut.files())

			rec := serveWith(h, ut.creationRequest(t, 10, metadata))
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

			upload, err := ut.store.Uploads().Get(t.Context(), ut.alice.ID, uploadID(rec.Header().Get("Location")))
			require.NoError(t, err)
			require.Empty(t, upload.FileName)
		})
	}
}

// Given a creation with a length above VERVE_MAX_UPLOAD_SIZE, a missing, empty or malformed length,
// an Upload-Defer-Length, a malformed Upload-Metadata or a Tus-Resumable other than 1.0.0, then it
// answers 413, 400 or 412 and stores nothing.
func TestUploads_Create_RefusesAnInvalidCreation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(r *http.Request)
		status int
	}{
		{name: "length above the limit", change: func(r *http.Request) { r.Header.Set("Upload-Length", "1001") }, status: http.StatusRequestEntityTooLarge},
		{name: "missing length", change: func(r *http.Request) { r.Header.Del("Upload-Length") }, status: http.StatusBadRequest},
		{name: "deferred length", change: func(r *http.Request) {
			r.Header.Del("Upload-Length")
			r.Header.Set("Upload-Defer-Length", "1")
		}, status: http.StatusBadRequest},
		{name: "length and deferred length", change: func(r *http.Request) { r.Header.Set("Upload-Defer-Length", "1") }, status: http.StatusBadRequest},
		{name: "length 0", change: func(r *http.Request) { r.Header.Set("Upload-Length", "0") }, status: http.StatusBadRequest},
		{name: "negative length", change: func(r *http.Request) { r.Header.Set("Upload-Length", "-1") }, status: http.StatusBadRequest},
		{name: "length not a number", change: func(r *http.Request) { r.Header.Set("Upload-Length", "1e3") }, status: http.StatusBadRequest},
		{name: "filename not base64", change: func(r *http.Request) { r.Header.Set("Upload-Metadata", "filename export.zip") }, status: http.StatusBadRequest},
		{name: "another tus version", change: func(r *http.Request) { r.Header.Set("Tus-Resumable", "0.2.2") }, status: http.StatusPreconditionFailed},
		{name: "no tus version", change: func(r *http.Request) { r.Header.Del("Tus-Resumable") }, status: http.StatusPreconditionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ut := newUploadsTest(t)
			h := ut.handler(t, ut.files())

			r := ut.creationRequest(t, 10, exportMetadata)
			tt.change(r)

			rec := serveWith(h, r)
			status, _ := problemCode(t, rec)
			require.Equal(t, tt.status, status)
			require.Equal(t, "1.0.0", rec.Header().Get("Tus-Resumable"))

			if tt.status == http.StatusPreconditionFailed {
				require.Equal(t, "1.0.0", rec.Header().Get("Tus-Version"))
			}

			ut.requireNoUpload(t)
		})
	}
}

// tusMethods are the methods of the tus endpoint.
var tusMethods = []string{http.MethodOptions, http.MethodPost, http.MethodHead, http.MethodPatch}

// Given a token, then every request of the tus endpoint answers 401, and so does a request without
// a session that passes the CSRF checks.
func TestUploads_TokenOrNoSession_Answers401(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)
	token, _ := ut.mustCreateToken(t, ut.alice, time.Time{}, store.ScopeIngest, store.ScopeRead)

	for _, method := range tusMethods {
		target := location
		if method == http.MethodOptions || method == http.MethodPost {
			target = uploadsPath
		}

		for _, auth := range []string{"token", "none"} {
			t.Run(method+" "+auth, func(t *testing.T) {
				r := tusRequest(t, method, target, strings.NewReader("0123456789"), "theme=dark")
				r.Header.Set("Upload-Length", "10")
				r.Header.Set("Upload-Offset", "0")
				r.Header.Set("Content-Type", "application/offset+octet-stream")

				if auth == "token" {
					r.Header.Del("Cookie")
					r.Header.Set("Authorization", "Bearer "+token)
				}

				rec := serveWith(h, r)
				require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
			})
		}
	}

	ids, err := ut.store.Uploads().IDs(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{uploadID(location)}, ids, "no request created or removed an upload")

	upload, err := ut.store.Uploads().Get(t.Context(), ut.alice.ID, uploadID(location))
	require.NoError(t, err)
	require.Zero(t, upload.Offset, "no request wrote to the upload")
}

// Given a session without the CSRF header or with another Origin, then POST, PATCH and DELETE are
// refused with 403 csrf_failed, as every state-changing request is.
func TestUploads_WithoutCSRFProtection_Answers403(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)

	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		target := location
		if method == http.MethodPost {
			target = uploadsPath
		}

		for _, missing := range []string{"csrf header", "origin"} {
			t.Run(method+" without "+missing, func(t *testing.T) {
				r := tusRequest(t, method, target, strings.NewReader("0123456789"), ut.aliceCookie)
				r.Header.Set("Upload-Length", "10")
				r.Header.Set("Upload-Offset", "0")
				r.Header.Set("Content-Type", "application/offset+octet-stream")

				if missing == "csrf header" {
					r.Header.Del("Verve-Csrf")
				} else {
					r.Header.Set("Origin", "https://evil.example")
				}

				status, code := problemCode(t, serveWith(h, r))
				require.Equal(t, http.StatusForbidden, status)
				require.Equal(t, "csrf_failed", code)
			})
		}
	}

	ids, err := ut.store.Uploads().IDs(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{uploadID(location)}, ids)
}

// Given HEAD on an upload, then it answers 200 with its offset, its length, its expiry and its
// metadata, never cached.
func TestUploads_Head_GivesTheOffset(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)

	rec := head(t, h, location, ut.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "1.0.0", rec.Header().Get("Tus-Resumable"))
	require.Equal(t, "0", rec.Header().Get("Upload-Offset"))
	require.Equal(t, "10", rec.Header().Get("Upload-Length"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "Thu, 05 Nov 2026 08:00:00 GMT", rec.Header().Get("Upload-Expires"))
	require.Equal(t, "filename ZXhwb3J0LnppcA==", rec.Header().Get("Upload-Metadata"))
	require.Empty(t, rec.Body.String())
}

// Given an upload of another user, an unknown one or an expired one, then HEAD answers 404.
func TestUploads_Head_OfAnotherUserUnknownOrExpired_Answers404(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)

	require.Equal(t, http.StatusNotFound, head(t, h, location, ut.bobCookie).Code)
	require.Equal(t, http.StatusNotFound, head(t, h, uploadsPath+"UNKNOWN", ut.aliceCookie).Code)

	ut.clock.Advance(24*time.Hour - time.Millisecond)
	require.Equal(t, http.StatusOK, head(t, h, location, ut.aliceCookie).Code)

	ut.clock.Advance(time.Millisecond)
	require.Equal(t, http.StatusNotFound, head(t, h, location, ut.aliceCookie).Code, "an expired upload answers 404")
}

// patchRequest returns the request of alice that writes body at offset of the upload of location.
func (ut uploadsTest) patchRequest(t *testing.T, location string, offset int64, body io.Reader) *http.Request {
	t.Helper()

	r := tusRequest(t, http.MethodPatch, location, body, ut.aliceCookie)
	r.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	r.Header.Set("Content-Type", "application/offset+octet-stream")

	return r
}

// patch serves the PATCH of alice that writes body at offset of the upload of location on h, and
// returns the response.
func (ut uploadsTest) patch(t *testing.T, h http.Handler, location string, offset int64, body string) *httptest.ResponseRecorder {
	t.Helper()

	return serveWith(h, ut.patchRequest(t, location, offset, strings.NewReader(body)))
}

// requireOffset requires that HEAD gives offset for the upload of location, and that its file holds
// content.
func (ut uploadsTest) requireOffset(t *testing.T, h http.Handler, location string, offset int64, content string) {
	t.Helper()

	rec := head(t, h, location, ut.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, strconv.FormatInt(offset, 10), rec.Header().Get("Upload-Offset"))

	data, err := os.ReadFile(filepath.Join(ut.uploadsDir, uploadID(location)))
	require.NoError(t, err)
	require.Equal(t, content, string(data))
}

// Given PATCH at the stored offset with application/offset+octet-stream, then it writes the bytes,
// stores the new offset and answers 204 with it and the new Upload-Expires, 24 hours after the
// write, and the next PATCH writes after them.
func TestUploads_Patch_WritesAtTheOffset(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)

	ut.clock.Advance(time.Hour)

	rec := ut.patch(t, h, location, 0, "01234")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, "1.0.0", rec.Header().Get("Tus-Resumable"))
	require.Equal(t, "5", rec.Header().Get("Upload-Offset"))
	require.Equal(t, "Thu, 05 Nov 2026 09:00:00 GMT", rec.Header().Get("Upload-Expires"))
	ut.requireOffset(t, h, location, 5, "01234")

	upload, err := ut.store.Uploads().Get(t.Context(), ut.alice.ID, uploadID(location))
	require.NoError(t, err)
	require.Equal(t, authTestNow.Add(25*time.Hour), upload.ExpiresAt, "every write moves the expiry")

	rec = ut.patch(t, h, location, 5, "567")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, "8", rec.Header().Get("Upload-Offset"))
	ut.requireOffset(t, h, location, 8, "01234567")
}

// Given an upload whose file holds bytes past its stored offset, such as after a crash between the
// sync of a write and the store of its offset, then PATCH writes at the stored offset over them.
func TestUploads_Patch_WritesOverTheBytesPastTheOffset(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)
	require.NoError(t, os.WriteFile(filepath.Join(ut.uploadsDir, uploadID(location)), []byte("0123456"), 0o600))

	rec := ut.patch(t, h, location, 0, "ab")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	ut.requireOffset(t, h, location, 2, "ab")
}

// Given PATCH with another content type, then it answers 415; with another offset 409; with a
// missing or malformed offset 400; and with a body past Upload-Length, whether it declares its
// length or not, 400; each without changing the offset.
func TestUploads_Patch_RefusesAnInvalidWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(r *http.Request)
		body   string
		status int
	}{
		{name: "another content type", change: func(r *http.Request) { r.Header.Set("Content-Type", "application/octet-stream") }, body: "012", status: http.StatusUnsupportedMediaType},
		{name: "no content type", change: func(r *http.Request) { r.Header.Del("Content-Type") }, body: "012", status: http.StatusUnsupportedMediaType},
		{name: "another offset", change: func(r *http.Request) { r.Header.Set("Upload-Offset", "2") }, body: "012", status: http.StatusConflict},
		{name: "no offset", change: func(r *http.Request) { r.Header.Del("Upload-Offset") }, body: "012", status: http.StatusBadRequest},
		{name: "malformed offset", change: func(r *http.Request) { r.Header.Set("Upload-Offset", "-1") }, body: "012", status: http.StatusBadRequest},
		{name: "declared body past the length", change: func(*http.Request) {}, body: "0123456789", status: http.StatusBadRequest},
		{name: "body past the length", change: func(r *http.Request) { r.ContentLength = -1 }, body: "0123456789", status: http.StatusBadRequest},
		{name: "another tus version", change: func(r *http.Request) { r.Header.Set("Tus-Resumable", "0.2.2") }, body: "012", status: http.StatusPreconditionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ut := newUploadsTest(t)
			h := ut.handler(t, ut.files())
			location := ut.mustCreate(t, h, 10)
			require.Equal(t, http.StatusNoContent, ut.patch(t, h, location, 0, "a").Code)

			r := ut.patchRequest(t, location, 1, strings.NewReader(tt.body))
			tt.change(r)

			rec := serveWith(h, r)
			status, _ := problemCode(t, rec)
			require.Equal(t, tt.status, status)

			// The bytes that a refused write left past the offset are written over.
			require.Equal(t, "1", head(t, h, location, ut.aliceCookie).Header().Get("Upload-Offset"))
			require.Equal(t, http.StatusNoContent, ut.patch(t, h, location, 1, "bc").Code)
			ut.requireOffset(t, h, location, 3, "abc")
		})
	}
}

// cutBody reads data, then fails as a body whose connection is lost.
type cutBody struct {
	data *strings.Reader
}

func (b cutBody) Read(p []byte) (int, error) {
	if b.data.Len() == 0 {
		return 0, io.ErrUnexpectedEOF
	}

	return b.data.Read(p)
}

// Given a PATCH whose body is cut, then the bytes received are synced and stored, so that the next
// HEAD gives where to resume, and the next PATCH resumes there.
func TestUploads_Patch_CutBody_StoresTheBytesReceived(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)

	r := ut.patchRequest(t, location, 0, cutBody{data: strings.NewReader("0123")})
	r.ContentLength = 10

	rec := serveWith(h, r)
	require.NotEqual(t, http.StatusNoContent, rec.Code)
	ut.requireOffset(t, h, location, 4, "0123")

	require.Equal(t, http.StatusNoContent, ut.patch(t, h, location, 4, "45").Code)
	ut.requireOffset(t, h, location, 6, "012345")
}

// Given a PATCH while another runs on the same upload, then it answers 423 and writes nothing, and
// the PATCH of another upload runs meanwhile.
func TestUploads_Patch_WhileAnotherRuns_Answers423(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, ut.files())
	location := ut.mustCreate(t, h, 10)
	other := ut.mustCreate(t, h, 10)

	body, writer := io.Pipe()
	done := make(chan *httptest.ResponseRecorder)

	r := ut.patchRequest(t, location, 0, body)

	go func() { done <- serveWith(h, r) }()

	// The write returns once the running PATCH reads it, holding the lock of the upload.
	_, err := writer.Write([]byte("01"))
	require.NoError(t, err)

	rec := ut.patch(t, h, location, 0, "ab")
	status, _ := problemCode(t, rec)
	require.Equal(t, http.StatusLocked, status)

	require.Equal(t, http.StatusNoContent, ut.patch(t, h, other, 0, "xy").Code)

	_, err = writer.Write([]byte("234"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	first := <-done
	require.Equal(t, http.StatusNoContent, first.Code, first.Body.String())
	ut.requireOffset(t, h, location, 5, "01234")

	require.Equal(t, http.StatusNoContent, ut.patch(t, h, location, 5, "5").Code, "the lock is released")
}

// fullUploads is the real files of the uploads on a disk that fills up after n bytes of each
// write.
type fullUploads struct {
	*ingest.Uploads

	n int64
}

func (f fullUploads) Append(ctx context.Context, name string, offset int64, body io.Reader) (int64, error) {
	written, err := f.Uploads.Append(ctx, name, offset, io.LimitReader(body, f.n))
	if err != nil {
		return written, err
	}

	return written, fmt.Errorf("write upload file: %w", syscall.ENOSPC)
}

// Given a disk full during a PATCH, then it answers 507 and the offset stays at the last synced
// byte, from which the client resumes once space is freed.
func TestUploads_Patch_DiskFull_Answers507(t *testing.T) {
	t.Parallel()

	ut := newUploadsTest(t)
	h := ut.handler(t, fullUploads{Uploads: ut.files(), n: 3})
	location := ut.mustCreate(t, h, 10)

	rec := ut.patch(t, h, location, 0, "012345")
	status, code := problemCode(t, rec)
	require.Equal(t, http.StatusInsufficientStorage, status)
	require.Equal(t, "internal", code)
	ut.requireOffset(t, h, location, 3, "012")

	h = ut.handler(t, ut.files())
	require.Equal(t, http.StatusNoContent, ut.patch(t, h, location, 3, "345").Code)
	ut.requireOffset(t, h, location, 6, "012345")
}
