package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
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
	// Complete moves the file of name into the inbox, and returns its name there.
	Complete(ctx context.Context, name string) (inboxName string, err error)
	// Return moves the inbox file of inboxName back to the file of name, undoing Complete.
	Return(ctx context.Context, name, inboxName string) error
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
// stored in its store with their files in its files, which complete into the inbox with a job of
// its jobs.
type tusUploads struct {
	store   store.Store
	files   UploadFiles
	jobs    auth.JobQueue
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
		jobs:    deps.Jobs,
		clock:   deps.Clock,
		maxSize: deps.MaxUploadSize,
		writing: map[string]bool{},
	}

	mux.HandleFunc("OPTIONS /api/v1/uploads/{$}", u.handle(false, u.options))
	mux.HandleFunc("POST /api/v1/uploads/{$}", u.handle(true, u.create))
	mux.HandleFunc("HEAD /api/v1/uploads/{id}", u.handle(true, u.head))
	mux.HandleFunc("PATCH /api/v1/uploads/{id}", u.handle(true, u.patch))
	mux.HandleFunc("DELETE /api/v1/uploads/{id}", u.handle(true, u.terminate))
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
	upload, err := u.findWithFile(r.Context(), principal, r.PathValue("id"))
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

	ctx := r.Context()
	id := r.PathValue("id")

	// Looked up before the lock, so that an upload of another user answers 404 whatever its lock.
	if _, err := u.findUnexpired(ctx, principal, id); err != nil {
		return err
	}

	if !u.lock(id) {
		return uploadLocked()
	}

	defer u.unlock(id)

	// Looked up again under the lock, for the offset that the write before stored.
	upload, err := u.findWithFile(ctx, principal, id)
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

	if errors.Is(err, fs.ErrNotExist) {
		return uploadNotFound()
	}

	// The bytes written are synced: the last one completes the upload, also when the body or the
	// request failed after it, so that the offset is never stored at the length.
	if upload.Offset+written == upload.Length {
		return u.complete(ctx, w, principal, upload)
	}

	if err == nil || written > 0 {
		// Stored also when the client went away with the context of the request.
		var advanceErr error

		upload, advanceErr = u.store.Uploads().Advance(context.WithoutCancel(ctx), principal.UserID, upload.ID,
			upload.Offset, upload.Offset+written, u.clock.Now().Add(uploadLifetime))

		switch {
		case errors.Is(advanceErr, store.ErrNotFound):
			// Removed meanwhile, such as by cleanup.uploads once expired.
			return uploadNotFound()
		case errors.Is(advanceErr, store.ErrConflict):
			return tusProblem(http.StatusConflict, "another request moved the offset of the upload")
		case advanceErr != nil:
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

// complete hands upload, whose last byte is synced, to the import pipeline (FR-4 of XML import): it
// moves its file into the inbox, then in one transaction deletes the upload and creates its import
// of origin xml, with the file name of the upload as the name of its client, and its job import.xml,
// and answers with the import id in Verve-Import-Id. Its offset is never stored at the length, so
// that a client resumes an upload that a crash left before the commit. A failure of the transaction
// returns the file to the upload, so that its client resumes it, once space is freed for a full
// disk.
func (u *tusUploads) complete(ctx context.Context, w http.ResponseWriter, principal auth.Principal, upload store.Upload) error {
	// Every byte is received: the import is created also when the client went away.
	ctx = context.WithoutCancel(ctx)

	inboxFile, err := u.files.Complete(ctx, upload.ID)
	if err != nil {
		return err
	}

	var client map[string]string
	if upload.FileName != "" {
		client = map[string]string{"name": upload.FileName}
	}

	var imp store.Import

	err = u.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := tx.Uploads().Delete(ctx, principal.UserID, upload.ID); err != nil {
			return err
		}

		var err error

		imp, err = queueImport(ctx, tx, u.jobs, store.NewImport{
			UserID:    principal.UserID,
			Origin:    store.OriginXML,
			Client:    client,
			InboxFile: inboxFile,
		}, func(importID int64) (store.NewJob, error) { return applexml.Job(principal.UserID, importID, "") })

		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		// Removed meanwhile: the returned file has no upload, and cleanup.uploads removes it.
		return errors.Join(uploadNotFound(), u.files.Return(ctx, upload.ID, inboxFile))
	}

	if err != nil {
		return errors.Join(fmt.Errorf("queue import of upload: %w", err), u.files.Return(ctx, upload.ID, inboxFile))
	}

	w.Header().Set("Upload-Offset", strconv.FormatInt(upload.Length, 10))
	w.Header().Set("Verve-Import-Id", strconv.FormatInt(imp.ID, 10))
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// terminate removes an upload and its file.
func (u *tusUploads) terminate(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	ctx := r.Context()
	id := r.PathValue("id")

	// The upload goes also when its file is gone. Looked up before the lock, so that an upload of
	// another user answers 404 whatever its lock.
	if _, err := u.findUnexpired(ctx, principal, id); err != nil {
		return err
	}

	if !u.lock(id) {
		return uploadLocked()
	}

	defer u.unlock(id)

	err := u.store.Uploads().Delete(ctx, principal.UserID, id)
	if errors.Is(err, store.ErrNotFound) {
		return uploadNotFound()
	}

	if err != nil {
		return err
	}

	// A file left behind is removed by cleanup.uploads.
	if err := u.files.Remove(ctx, id); err != nil {
		return err
	}

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

// findWithFile returns the upload of id of the user of principal, or a 404 problem when it is unknown,
// expired or of another user, or when its file is gone, such as after a crash between the rename
// of its last write and the commit of its import (FR-4 of XML import).
func (u *tusUploads) findWithFile(ctx context.Context, principal auth.Principal, id string) (store.Upload, error) {
	upload, err := u.findUnexpired(ctx, principal, id)
	if err != nil {
		return store.Upload{}, err
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

// findUnexpired returns the upload of id of the user of principal, or a 404 problem when it is
// unknown, expired (FR-3 of XML import) or of another user.
func (u *tusUploads) findUnexpired(ctx context.Context, principal auth.Principal, id string) (store.Upload, error) {
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

	if isOutOfStorage(err) {
		WriteProblem(w, *tusProblem(http.StatusInsufficientStorage, outOfStorageDetail))

		return
	}

	ctx := r.Context()
	logging.FromContext(ctx).ErrorContext(ctx, "serve upload", slog.Any("error", err))
	WriteProblem(w, *tusProblem(http.StatusInternalServerError, ""))
}

// uploadLocked returns the 423 problem of an upload that another request holds.
func uploadLocked() *Problem {
	return tusProblem(http.StatusLocked, "another request writes to the upload")
}
