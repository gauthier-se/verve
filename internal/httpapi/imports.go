package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/logging"
	"github.com/gauthier-se/verve/internal/store"
)

// errImportFinished is the code of the cancellation of an import that ended.
var errImportFinished = errors.New("import_finished")

// ImportSummary is an import as the list gives it: with its total counters only.
type ImportSummary struct {
	ID         int64               `json:"id" example:"42" doc:"The id of the import."`
	Origin     string              `json:"origin" enum:"xml,hae,native,cli" example:"native" doc:"Where the data comes from: the Apple Health export, Health Auto Export, the native format or the command line."`
	Status     string              `json:"status" enum:"queued,running,completed,failed,cancelled" example:"completed" doc:"The status of the import."`
	Client     map[string]string   `json:"client,omitempty" doc:"The metadata of the client that sent the data, left out when there is none."`
	TokenName  string              `json:"tokenName,omitempty" example:"Verve for iOS" doc:"The name of the token that pushed the data, left out when there is none."`
	CreatedAt  time.Time           `json:"createdAt" doc:"When the import was created."`
	StartedAt  *time.Time          `json:"startedAt,omitempty" doc:"When the import first ran, left out until it does."`
	FinishedAt *time.Time          `json:"finishedAt,omitempty" doc:"When the import ended, left out until it does."`
	Report     ImportSummaryReport `json:"report" doc:"The total counters of the import so far."`
}

// ImportSummaryReport is the report of an import as the list gives it.
type ImportSummaryReport struct {
	Total ImportCounters `json:"total" doc:"Every counter, for every record."`
}

// importsInput is the input of GET /api/v1/imports.
type importsInput struct {
	PageParams

	Status store.ImportStatus `query:"status" enum:"queued,running,completed,failed,cancelled" example:"running" doc:"Keeps the imports of this status, left out for every import."`
}

// importsOutput is the response of GET /api/v1/imports.
type importsOutput struct {
	Body Page[ImportSummary]
}

// importInput is the input of the operations on one import.
type importInput struct {
	ID int64 `path:"id" doc:"The id of the import."`
}

// importOutput is the response of the operations on one import.
type importOutput struct {
	Body Import
}

// registerImports registers GET /api/v1/imports and GET /api/v1/imports/{id}, which read the
// imports of the user of the request in deps.Store, and POST /api/v1/imports/{id}/cancel, which
// cancels one, removing its file from deps.Inbox when it ends it.
func registerImports(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-imports",
		Method:      http.MethodGet,
		Path:        "/api/v1/imports",
		Summary:     "List the imports",
		Description: "Returns the imports of the current user, newest first by creation, with their total " +
			"counters. `GET /api/v1/imports/{id}` gives the whole report of one. Accepts a session or a token " +
			"with the `read` scope.",
		Tags:     []string{"Ingestion"},
		Security: scopeSecurity(store.ScopeRead),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *importsInput) (*importsOutput, error) {
		principal, err := requireScope(ctx, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		imports, next, err := listPage(in.PageParams, func(page store.PageRequest) ([]store.Import, error) {
			return deps.Store.Imports().List(ctx, principal.UserID, store.ImportsFilter{Status: in.Status}, page)
		}, store.Import.Cursor)
		if err != nil {
			return nil, fmt.Errorf("list imports: %w", err)
		}

		names, err := tokenNames(ctx, deps.Store, principal.UserID, imports...)
		if err != nil {
			return nil, err
		}

		items := make([]ImportSummary, 0, len(imports))
		for _, imp := range imports {
			items = append(items, importSummaryOf(importOf(imp, names[imp.TokenID])))
		}

		return &importsOutput{Body: Page[ImportSummary]{Items: items, NextCursor: next}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-import",
		Method:      http.MethodGet,
		Path:        "/api/v1/imports/{id}",
		Summary:     "Get an import",
		Description: "Returns an import of the current user with its whole report: the counters in total, per " +
			"type key and for the workouts, and the sample error messages. Accepts a session or a token with " +
			"the `ingest` or `read` scope, so that a client can follow the import it pushed.",
		Tags:     []string{"Ingestion"},
		Security: scopeSecurity(store.ScopeIngest, store.ScopeRead),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *importInput) (*importOutput, error) {
		principal, err := requireScope(ctx, store.ScopeIngest, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		imp, err := deps.Store.Imports().Get(ctx, principal.UserID, in.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, huma.NewError(http.StatusNotFound, "")
		}

		if err != nil {
			return nil, fmt.Errorf("get import: %w", err)
		}

		names, err := tokenNames(ctx, deps.Store, principal.UserID, imp)
		if err != nil {
			return nil, err
		}

		return &importOutput{Body: importOf(imp, names[imp.TokenID])}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancel-import",
		Method:      http.MethodPost,
		Path:        "/api/v1/imports/{id}/cancel",
		Summary:     "Cancel an import",
		Description: "Cancels a queued or running import of the current user, and answers 202 with the import. A " +
			"queued import ends cancelled at once, and a running one at the next batch boundary of its job: " +
			"the data already stored stays. A finished import answers 409 `import_finished`.",
		Tags:          []string{"Ingestion"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *importInput) (*importOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		imp, err := cancelImport(ctx, deps, principal.UserID, in.ID)

		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, huma.NewError(http.StatusNotFound, "")
		case errors.Is(err, errImportFinished):
			return nil, codedProblem(http.StatusConflict, errImportFinished)
		case err != nil:
			return nil, err
		}

		names, err := tokenNames(ctx, deps.Store, principal.UserID, imp)
		if err != nil {
			return nil, err
		}

		return &importOutput{Body: importOf(imp, names[imp.TokenID])}, nil
	})
}

// cancelImport cancels the import of id of userID (FR-10 of Ingestion pipeline) and returns it. It
// cancels the job of the import: a running job ends the import at its next batch boundary. An
// import that no handler can end, its job queued, waiting for a retry or gone, ends cancelled here,
// recording its end with the client of the request, and its inbox file is removed. It returns
// store.ErrNotFound for an import that userID does not have, and errImportFinished for one that
// ended.
func cancelImport(ctx context.Context, deps APIDeps, userID, id int64) (store.Import, error) {
	var (
		imp   store.Import
		ended bool
	)

	err := deps.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		var err error

		imp, err = tx.Imports().Get(ctx, userID, id)
		if err != nil {
			return err
		}

		if imp.Status.Finished() {
			return errImportFinished
		}

		running, err := cancelJobOf(ctx, tx, imp)
		if err != nil || running {
			return err
		}

		imp, err = tx.Imports().SetStatus(ctx, userID, id, store.ImportCancelled)
		if err != nil {
			return err
		}

		ended = true

		return audit.Record(ctx, tx.AuditEvents(), audit.ImportCancelled(userID, id, imp.Origin, imp.Report.Total))
	})
	if err != nil {
		return store.Import{}, fmt.Errorf("cancel import %d: %w", id, err)
	}

	if ended && imp.InboxFile != "" {
		// The import is cancelled: a file left behind only takes space.
		if err := deps.Inbox.Remove(context.WithoutCancel(ctx), imp.InboxFile); err != nil {
			logging.FromContext(ctx).WarnContext(ctx, "remove inbox file of cancelled import",
				slog.Int64("import", id), slog.Any("error", err))
		}
	}

	return imp, nil
}

// cancelJobOf cancels the active job of imp through tx, and reports whether it is still running,
// its handler then ending the import. A queued job, such as one waiting for its retry, is
// cancelled at once, and an import without an active job has none to cancel.
func cancelJobOf(ctx context.Context, tx store.Repositories, imp store.Import) (running bool, err error) {
	job, err := ingest.JobOfImport(ctx, tx.Jobs(), imp.UserID, imp.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if err := tx.Jobs().Cancel(ctx, job.ID); err != nil {
		return false, fmt.Errorf("cancel job %d: %w", job.ID, err)
	}

	return job.Status == store.JobRunning, nil
}

// tokenNames returns the names of the API tokens of userID by id, when one of imports was pushed by
// a token, and nil otherwise.
func tokenNames(ctx context.Context, repos store.Repositories, userID int64, imports ...store.Import) (map[int64]string, error) {
	if !slices.ContainsFunc(imports, func(imp store.Import) bool { return imp.TokenID != 0 }) {
		return nil, nil
	}

	tokens, err := repos.APITokens().ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}

	names := make(map[int64]string, len(tokens))
	for _, token := range tokens {
		names[token.ID] = token.Name
	}

	return names, nil
}

// importSummaryOf returns imp as the list gives it.
func importSummaryOf(imp Import) ImportSummary {
	return ImportSummary{
		ID:         imp.ID,
		Origin:     imp.Origin,
		Status:     imp.Status,
		Client:     imp.Client,
		TokenName:  imp.TokenName,
		CreatedAt:  imp.CreatedAt,
		StartedAt:  imp.StartedAt,
		FinishedAt: imp.FinishedAt,
		Report:     ImportSummaryReport{Total: imp.Report.Total},
	}
}

// Import is an import of the user of the request, never its token or its payload (Ingestion
// pipeline, Data and API notes).
type Import struct {
	ID         int64             `json:"id" example:"42" doc:"The id of the import."`
	Origin     string            `json:"origin" enum:"xml,hae,native,cli" example:"native" doc:"Where the data comes from: the Apple Health export, Health Auto Export, the native format or the command line."`
	Status     string            `json:"status" enum:"queued,running,completed,failed,cancelled" example:"queued" doc:"The status of the import."`
	Client     map[string]string `json:"client,omitempty" doc:"The metadata of the client that sent the data, left out when there is none. A push stores it once its job reads the payload."`
	TokenName  string            `json:"tokenName,omitempty" example:"Verve for iOS" doc:"The name of the token that pushed the data, left out when there is none."`
	CreatedAt  time.Time         `json:"createdAt" doc:"When the import was created."`
	StartedAt  *time.Time        `json:"startedAt,omitempty" doc:"When the import first ran, left out until it does."`
	FinishedAt *time.Time        `json:"finishedAt,omitempty" doc:"When the import ended, left out until it does."`
	Report     ImportReport      `json:"report" doc:"What happened to the records of the import so far."`
}

// ImportReport is the report of an import: its counters in total, per type key and for the
// workouts, and its sample error messages, which never hold a value.
type ImportReport struct {
	Total    ImportCounters                `json:"total" doc:"Every counter, for every record."`
	Types    map[string]ImportTypeCounters `json:"types" nullable:"false" doc:"The counters of each type key, a metric type key or the identifier of an unsupported type."`
	Workouts ImportTypeCounters            `json:"workouts" doc:"The counters of the workouts."`
	Errors   []string                      `json:"errors" nullable:"false" doc:"Up to 100 sample error messages, which name the position, the type and the reason of a record."`
}

// ImportCounters counts the records of an import by what happened to them, every counter given.
type ImportCounters struct {
	Received    int64 `json:"received" doc:"Samples and workouts read from the payload."`
	Inserted    int64 `json:"inserted" doc:"Records stored for the first time."`
	Updated     int64 `json:"updated" doc:"Records that replaced a stored one."`
	Duplicates  int64 `json:"duplicates" doc:"Records identical to a stored one."`
	Blocked     int64 `json:"blocked" doc:"Records dropped by a block rule."`
	Invalid     int64 `json:"invalid" doc:"Records rejected, such as one with a missing field or an unknown unit."`
	Unsupported int64 `json:"unsupported" doc:"Records of known but excluded types, such as ECG."`
	Suspicious  int64 `json:"suspicious" doc:"Stored records that start before 2000 or more than one day after the import was created."`
	Warnings    int64 `json:"warnings" doc:"Stored records that lost a detail."`
	Deleted     int64 `json:"deleted" doc:"Records removed by a deletion."`
	NotFound    int64 `json:"notFound" doc:"Deleted uuids that matched no record."`
}

// ImportTypeCounters counts the records of a type key, or the workouts, of an import, each counter
// at zero left out.
type ImportTypeCounters struct {
	Received    int64 `json:"received,omitempty"`
	Inserted    int64 `json:"inserted,omitempty"`
	Updated     int64 `json:"updated,omitempty"`
	Duplicates  int64 `json:"duplicates,omitempty"`
	Blocked     int64 `json:"blocked,omitempty"`
	Invalid     int64 `json:"invalid,omitempty"`
	Unsupported int64 `json:"unsupported,omitempty"`
	Suspicious  int64 `json:"suspicious,omitempty"`
	Warnings    int64 `json:"warnings,omitempty"`
	Deleted     int64 `json:"deleted,omitempty"`
	NotFound    int64 `json:"notFound,omitempty"`
}

// importOf returns imp as the API answers it, with tokenName, the name of its token or empty.
func importOf(imp store.Import, tokenName string) Import {
	out := Import{
		ID:        imp.ID,
		Origin:    string(imp.Origin),
		Status:    string(imp.Status),
		Client:    imp.Client,
		TokenName: tokenName,
		CreatedAt: imp.CreatedAt,
		Report: ImportReport{
			Total:    ImportCounters(imp.Report.Total),
			Types:    make(map[string]ImportTypeCounters, len(imp.Report.Types)),
			Workouts: ImportTypeCounters(imp.Report.Workouts),
			Errors:   imp.Report.Errors,
		},
	}

	for key, counters := range imp.Report.Types {
		out.Report.Types[key] = ImportTypeCounters(counters)
	}

	if out.Report.Errors == nil {
		out.Report.Errors = []string{}
	}

	if !imp.StartedAt.IsZero() {
		out.StartedAt = &imp.StartedAt
	}

	if !imp.FinishedAt.IsZero() {
		out.FinishedAt = &imp.FinishedAt
	}

	return out
}
