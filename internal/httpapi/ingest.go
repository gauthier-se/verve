package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"syscall"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/ingest/native"
	"github.com/gauthier-se/verve/internal/store"
)

// Inbox stores the bodies of pushes until their import no longer needs them, as *ingest.Inbox does.
type Inbox interface {
	// Write stores body durably and returns the name of its file. On failure it leaves no file, and
	// its error wraps syscall.ENOSPC when the disk is full.
	Write(ctx context.Context, body io.Reader) (name string, err error)
	// Remove removes the file of name.
	Remove(name string) error
}

// pushInput is the input of POST /api/v1/ingest. It has no Body field, so Huma reads no body:
// Resolve keeps the request, whose body the operation streams to the inbox once it is authorized.
type pushInput struct {
	request *http.Request
	writer  http.ResponseWriter
}

// Resolve keeps the request and its response writer.
func (in *pushInput) Resolve(ctx huma.Context) []error {
	in.request, in.writer = humago.Unwrap(ctx)

	return nil
}

// pushOutput is the response of POST /api/v1/ingest.
type pushOutput struct {
	Body Import
}

// registerIngest registers POST /api/v1/ingest, which writes the body of a native push to
// deps.Inbox, then creates its import and its job import.native in deps.Store, in one transaction,
// with deps.Jobs.
func registerIngest(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "push-native",
		Method:      http.MethodPost,
		Path:        "/api/v1/ingest",
		Summary:     "Push data in the native format",
		Description: "Stores a payload of the native format `verve.ingest.v1` for processing, and answers 202 with " +
			"its queued import, which `GET /api/v1/imports/{id}` follows. A 202 means that the payload is safely " +
			"stored and will be processed. The request only checks the authentication and the size of the body, " +
			"bounded by `VERVE_MAX_INGEST_BODY`: a larger body answers 413. The import validates the payload, and " +
			"fails as a whole for a body that is not a payload of the format. A full disk answers 507: retry " +
			"later. Accepts a session or a token with the `ingest` scope.",
		Tags:          []string{"Ingestion"},
		Security:      scopeSecurity(store.ScopeIngest),
		DefaultStatus: http.StatusAccepted,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestEntityTooLarge,
			http.StatusInsufficientStorage,
		},
		// Without a schema, so that Huma neither reads nor validates the body, which the operation streams.
		RequestBody: &huma.RequestBody{
			Description: "A payload of the format `verve.ingest.v1`, described in the specification of the " +
				"native format. It is validated by the import, not by the request.",
			Required: true,
			Content:  map[string]*huma.MediaType{"application/json": {}},
		},
	}, func(ctx context.Context, in *pushInput) (*pushOutput, error) {
		principal, err := requireScope(ctx, store.ScopeIngest)
		if err != nil {
			return nil, err
		}

		tokenName, err := nameOfToken(ctx, deps.Store, principal)
		if err != nil {
			return nil, err
		}

		imp, err := acceptPush(ctx, deps, principal, in)
		if err != nil {
			return nil, err
		}

		return &pushOutput{Body: importOf(imp, tokenName)}, nil
	})
}

// acceptPush writes the body of the request of in to the inbox, then creates its import and its job,
// and returns the import. A failure leaves no file.
func acceptPush(ctx context.Context, deps APIDeps, principal auth.Principal, in *pushInput) (store.Import, error) {
	if in.request.ContentLength > deps.MaxIngestBody {
		return store.Import{}, bodyTooLarge(deps.MaxIngestBody)
	}

	name, err := deps.Inbox.Write(ctx, http.MaxBytesReader(in.writer, in.request.Body, deps.MaxIngestBody))
	if err != nil {
		return store.Import{}, pushError(err, deps.MaxIngestBody)
	}

	var imp store.Import

	err = deps.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		var err error

		imp, err = tx.Imports().Create(ctx, store.NewImport{
			UserID:    principal.UserID,
			Origin:    store.OriginNative,
			TokenID:   principal.TokenID,
			InboxFile: name,
		})
		if err != nil {
			return err
		}

		job, err := native.Job(principal.UserID, imp.ID)
		if err != nil {
			return err
		}

		_, err = deps.Jobs.Enqueue(ctx, tx, job)

		return err
	})
	if err != nil {
		err = fmt.Errorf("queue import of push: %w", err)

		return store.Import{}, pushError(errors.Join(err, deps.Inbox.Remove(name)), deps.MaxIngestBody)
	}

	return imp, nil
}

// pushError returns the response of a push that failed with err: 413 for a body larger than
// maxBody, 507 for a full disk, and err otherwise.
func pushError(err error, maxBody int64) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return bodyTooLarge(maxBody)
	}

	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return huma.NewError(http.StatusInsufficientStorage, "the server is out of storage, retry later")
	}

	return err
}

// bodyTooLarge returns the 413 problem of a body larger than maxBody.
func bodyTooLarge(maxBody int64) error {
	return huma.NewError(http.StatusRequestEntityTooLarge,
		"the body is larger than "+strconv.FormatInt(maxBody, 10)+" bytes, the limit of VERVE_MAX_INGEST_BODY")
}

// nameOfToken returns the name of the token of principal, or empty for a session.
func nameOfToken(ctx context.Context, s store.Store, principal auth.Principal) (string, error) {
	if principal.Method != auth.MethodToken {
		return "", nil
	}

	tokens, err := s.APITokens().ListByUser(ctx, principal.UserID)
	if err != nil {
		return "", fmt.Errorf("list tokens: %w", err)
	}

	for _, token := range tokens {
		if token.ID == principal.TokenID {
			return token.Name, nil
		}
	}

	return "", nil
}
