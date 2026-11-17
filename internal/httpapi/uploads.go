package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/logging"
	"github.com/gauthier-se/verve/internal/store"
)

// UploadFiles holds the files of the incomplete uploads, as *ingest.Uploads does.
type UploadFiles interface {
	// Create creates the empty file of a new upload and returns its name, a random one.
	Create(ctx context.Context) (name string, err error)
	// Exists reports whether the file of name exists.
	Exists(ctx context.Context, name string) (bool, error)
	// Append writes body at offset of the file of name, syncs it and returns the number of bytes
	// written and synced, also on failure. Its error wraps the error of body when body fails,
	// syscall.ENOSPC when the disk is full, and fs.ErrNotExist when the file is gone.
	Append(ctx context.Context, name string, offset int64, body io.Reader) (int64, error)
	// Remove removes the file of name, and succeeds when it is already gone.
	Remove(ctx context.Context, name string) error
}

const (
	// tusVersion is the only version of the tus protocol that the endpoint serves.
	tusVersion = "1.0.0"
	// tusExtensions are the extensions of the tus protocol that the endpoint serves (FR-2 of XML
	// import).
	tusExtensions = "creation,termination,expiration"
	// uploadLifetime is how long an incomplete upload lives after its last write (FR-3 of XML
	// import).
	uploadLifetime = 24 * time.Hour
	// uploadsPathPrefix starts the location of every upload.
	uploadsPathPrefix = "/api/v1/uploads/"
)

// tusUploads serves the tus endpoint under uploadsPathPrefix: the incomplete uploads of the sessions,
// stored in its store with their files in its files.
type tusUploads struct {
	store   store.Store
	files   UploadFiles
	clock   clock.Clock
	maxSize int64

	// mu guards writing, the ids of the uploads that a PATCH holds. The server is one process, which
	// the lock of the data directory ensures.
	mu      sync.Mutex
	writing map[string]bool
}

// registerUploads registers the tus endpoint on mux, a plain net/http handler (ADR 0015) behind the
// session and CSRF middlewares of every request.
func registerUploads(mux *http.ServeMux, deps APIDeps) {
	u := &tusUploads{
		store:   deps.Store,
		files:   deps.Uploads,
		clock:   deps.Clock,
		maxSize: deps.MaxUploadSize,
		writing: map[string]bool{},
	}

	mux.HandleFunc("OPTIONS /api/v1/uploads/{$}", u.handle(false, u.options))
	mux.HandleFunc("POST /api/v1/uploads/{$}", u.handle(true, u.create))
	mux.HandleFunc("HEAD /api/v1/uploads/{id}", u.handle(true, u.head))
	mux.HandleFunc("PATCH /api/v1/uploads/{id}", u.handle(true, u.patch))
}

// tusHandler serves a request of the tus endpoint for the user of principal.
type tusHandler func(w http.ResponseWriter, r *http.Request, principal auth.Principal) error

// handle returns the handler of next: it answers with Tus-Resumable, refuses a request without a
// session with 401 and, when versioned, a request without Tus-Resumable: 1.0.0 with 412, then
// answers the error of next.
func (u *tusUploads) handle(versioned bool, next tusHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Tus-Resumable", tusVersion)

		err := func() error {
			principal, err := requireUser(r.Context())
			if err != nil {
				return err
			}

			if versioned && r.Header.Get("Tus-Resumable") != tusVersion {
				w.Header().Set("Tus-Version", tusVersion)

				return tusProblem(http.StatusPreconditionFailed, "the endpoint serves the tus protocol "+tusVersion)
			}

			return next(w, r, principal)
		}()
		if err != nil {
			writeTusError(w, r, err)
		}
	}
}

// options describes the tus server.
func (u *tusUploads) options(w http.ResponseWriter, _ *http.Request, _ auth.Principal) error {
	w.Header().Set("Tus-Version", tusVersion)
	w.Header().Set("Tus-Extension", tusExtensions)
	w.Header().Set("Tus-Max-Size", strconv.FormatInt(u.maxSize, 10))
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// create creates an upload of Upload-Length bytes, with the filename of Upload-Metadata, and its
// empty file.
func (u *tusUploads) create(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	if _, ok := r.Header["Upload-Defer-Length"]; ok {
		return tusProblem(http.StatusBadRequest, "Upload-Defer-Length is not supported: send Upload-Length")
	}

	length, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
	if err != nil || length < 1 {
		return tusProblem(http.StatusBadRequest, "Upload-Length must be a number of bytes of at least 1")
	}

	if length > u.maxSize {
		return tusProblem(http.StatusRequestEntityTooLarge,
			"the upload is larger than "+strconv.FormatInt(u.maxSize, 10)+" bytes, the limit of VERVE_MAX_UPLOAD_SIZE")
	}

	fileName, err := metadataFileName(r.Header.Get("Upload-Metadata"))
	if err != nil {
		return tusProblem(http.StatusBadRequest, err.Error())
	}

	ctx := r.Context()

	name, err := u.files.Create(ctx)
	if err != nil {
		return err
	}

	upload, err := u.store.Uploads().Create(ctx, store.NewUpload{
		ID:        name,
		UserID:    principal.UserID,
		FileName:  fileName,
		Length:    length,
		ExpiresAt: u.clock.Now().Add(uploadLifetime),
	})
	if err != nil {
		return errors.Join(fmt.Errorf("create upload: %w", err), u.files.Remove(context.WithoutCancel(ctx), name))
	}

	w.Header().Set("Location", uploadsPathPrefix+upload.ID)
	setUploadExpires(w, upload)
	w.WriteHeader(http.StatusCreated)

	return nil
}

// head gives the offset of an upload, where its client resumes it.
func (u *tusUploads) head(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	upload, err := u.find(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		return err
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Upload-Offset", strconv.FormatInt(upload.Offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(upload.Length, 10))
	setUploadExpires(w, upload)

	if upload.FileName != "" {
		w.Header().Set("Upload-Metadata", "filename "+base64.StdEncoding.EncodeToString([]byte(upload.FileName)))
	}

	w.WriteHeader(http.StatusOK)

	return nil
}

// patch writes the body at the offset of an upload, which the request must give, and stores the
// new offset once the bytes are synced. The bytes of a body cut short or of a disk that fills up are
// stored as well, so that the client resumes after them. A body past the length of the upload
// changes nothing.
func (u *tusUploads) patch(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/offset+octet-stream" {
		return tusProblem(http.StatusUnsupportedMediaType, "the body must be application/offset+octet-stream")
	}

	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		return tusProblem(http.StatusBadRequest, "Upload-Offset must be a number of bytes")
	}

	id := r.PathValue("id")
	if !u.lock(id) {
		return tusProblem(http.StatusLocked, "another request writes to the upload")
	}

	defer u.unlock(id)

	ctx := r.Context()

	upload, err := u.find(ctx, principal, id)
	if err != nil {
		return err
	}

	if offset != upload.Offset {
		return tusProblem(http.StatusConflict, "Upload-Offset must be the offset of the upload, "+strconv.FormatInt(upload.Offset, 10))
	}

	left := upload.Length - upload.Offset
	if r.ContentLength > left {
		return bodyPastLength()
	}

	body := &uploadBody{r: r.Body, left: left}
	written, err := u.files.Append(ctx, upload.ID, upload.Offset, body)

	if body.pastLength {
		return bodyPastLength()
	}

	if err == nil || written > 0 {
		// Stored also when the client went away with the context of the request.
		var advanceErr error

		upload, advanceErr = u.store.Uploads().Advance(context.WithoutCancel(ctx), principal.UserID, upload.ID,
			upload.Offset, upload.Offset+written, u.clock.Now().Add(uploadLifetime))
		if advanceErr != nil {
			return errors.Join(err, advanceErr)
		}
	}

	if body.err != nil {
		return tusProblem(http.StatusBadRequest, "the body was cut: resume from Upload-Offset")
	}

	if err != nil {
		return err
	}

	w.Header().Set("Upload-Offset", strconv.FormatInt(upload.Offset, 10))
	setUploadExpires(w, upload)
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// lock reserves the upload of id for one write, and reports whether no other write holds it.
func (u *tusUploads) lock(id string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.writing[id] {
		return false
	}

	u.writing[id] = true

	return true
}

// unlock releases the upload of id that lock reserved.
func (u *tusUploads) unlock(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	delete(u.writing, id)
}

// errPastLength is the error of a body that goes past the length of its upload.
var errPastLength = errors.New("the body goes past Upload-Length")

// uploadBody reads the body of a PATCH up to the bytes left of its upload. It records whether the
// body goes past them, failing once it read them, and the error of the body, such as a cut
// connection.
type uploadBody struct {
	r    io.Reader
	left int64
	// pastLength reports whether the body holds more than left bytes.
	pastLength bool
	// err is the error of r, other than io.EOF.
	err error
}

func (b *uploadBody) Read(p []byte) (int, error) {
	if b.left == 0 {
		// One more byte tells a body past the length from its end.
		var extra [1]byte

		n, err := io.ReadFull(b.r, extra[:])
		if n > 0 {
			b.pastLength = true

			return 0, errPastLength
		}

		return 0, b.fail(err)
	}

	if int64(len(p)) > b.left {
		p = p[:b.left]
	}

	n, err := b.r.Read(p)
	b.left -= int64(n)

	return n, b.fail(err)
}

// fail records err unless it is io.EOF, and returns it.
func (b *uploadBody) fail(err error) error {
	if err != nil && !errors.Is(err, io.EOF) {
		b.err = err
	}

	return err
}

// bodyPastLength returns the 400 problem of a body that goes past the length of its upload.
func bodyPastLength() *Problem {
	return tusProblem(http.StatusBadRequest, "the body goes past Upload-Length")
}

// find returns the upload of id of the user of principal, or a 404 problem when it is unknown,
// expired or of another user, or when its file is gone, such as after a crash between the rename
// of its last write and the commit of its import (FR-4 of XML import).
func (u *tusUploads) find(ctx context.Context, principal auth.Principal, id string) (store.Upload, error) {
	upload, err := u.store.Uploads().Get(ctx, principal.UserID, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Upload{}, uploadNotFound()
	}

	if err != nil {
		return store.Upload{}, err
	}

	if !u.clock.Now().Before(upload.ExpiresAt) {
		return store.Upload{}, uploadNotFound()
	}

	exists, err := u.files.Exists(ctx, upload.ID)
	if err != nil {
		return store.Upload{}, err
	}

	if !exists {
		return store.Upload{}, uploadNotFound()
	}

	return upload, nil
}

// setUploadExpires sets Upload-Expires to the expiry of upload, in the HTTP date format.
func setUploadExpires(w http.ResponseWriter, upload store.Upload) {
	w.Header().Set("Upload-Expires", upload.ExpiresAt.UTC().Format(http.TimeFormat))
}

// metadataFileName returns the filename of the Upload-Metadata header metadata, a list of keys,
// each with a base64 value or none, separated by commas. It returns empty when metadata has no
// filename or a filename without value, and an error when metadata is malformed.
func metadataFileName(metadata string) (string, error) {
	if strings.TrimSpace(metadata) == "" {
		return "", nil
	}

	var fileName string

	for pair := range strings.SplitSeq(metadata, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(pair), " ")
		if key == "" {
			return "", errors.New("Upload-Metadata has an empty key")
		}

		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return "", fmt.Errorf("the value of %q in Upload-Metadata is not base64", key)
		}

		if key == "filename" {
			if !utf8.Valid(decoded) {
				return "", errors.New("the filename of Upload-Metadata is not UTF-8")
			}

			fileName = string(decoded)
		}
	}

	return fileName, nil
}

// tusProblem returns the problem of status with detail, its code defaulting from its status.
func tusProblem(status int, detail string) *Problem {
	return &Problem{Status: status, Detail: detail, Code: defaultCode(status)}
}

// uploadNotFound returns the 404 problem of an upload that is unknown, expired or of another user.
func uploadNotFound() *Problem {
	return tusProblem(http.StatusNotFound, "")
}

// writeTusError answers err: as its problem, 507 for a full disk, or 500 for any other error, which
// it logs.
func writeTusError(w http.ResponseWriter, r *http.Request, err error) {
	if p, ok := errors.AsType[*Problem](err); ok {
		WriteProblem(w, *p)

		return
	}

	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) || errors.Is(err, store.ErrFull) {
		WriteProblem(w, Problem{
			Status: http.StatusInsufficientStorage,
			Detail: "the server is out of storage, retry later",
			Code:   defaultCode(http.StatusInsufficientStorage),
		})

		return
	}

	ctx := r.Context()
	logging.FromContext(ctx).ErrorContext(ctx, "serve upload", slog.Any("error", err))
	WriteProblem(w, Problem{Status: http.StatusInternalServerError, Code: defaultCode(http.StatusInternalServerError)})
}
