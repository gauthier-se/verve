package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// imports implements store.ImportsRepository.
type imports struct {
	q   queries
	clk clock.Clock
}

func (r imports) Create(ctx context.Context, imp store.NewImport) (store.Import, error) {
	if !imp.Origin.Known() {
		return store.Import{}, fmt.Errorf("create import of user %d: origin %q: %w", imp.UserID, imp.Origin, store.ErrInvalid)
	}

	client, err := encodeClient(imp.Client)
	if err != nil {
		return store.Import{}, fmt.Errorf("create import of user %d: %w", imp.UserID, err)
	}

	var row sqlcgen.Import

	err = r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.CreateImport(ctx, sqlcgen.CreateImportParams{
			UserID:    imp.UserID,
			Origin:    string(imp.Origin),
			TokenID:   sql.NullInt64{Int64: imp.TokenID, Valid: imp.TokenID != 0},
			Client:    client,
			InboxFile: sql.NullString{String: imp.InboxFile, Valid: imp.InboxFile != ""},
			CreatedAt: toMillis(r.clk.Now()),
		})

		// The only foreign key of an import is its user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		return q.PurgeUserImports(ctx, sqlcgen.PurgeUserImportsParams{UserID: imp.UserID, Kept: store.ImportHistoryLimit})
	})
	if err != nil {
		return store.Import{}, fmt.Errorf("create import of user %d: %w", imp.UserID, err)
	}

	return importOf(row)
}

func (r imports) Get(ctx context.Context, userID, id int64) (store.Import, error) {
	row, err := r.q.read().GetUserImport(ctx, sqlcgen.GetUserImportParams{UserID: userID, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Import{}, fmt.Errorf("get import %d of user %d: %w", id, userID, err)
	}

	return importOf(row)
}

func (r imports) List(ctx context.Context, userID int64, filter store.ImportsFilter, page store.PageRequest) ([]store.Import, error) {
	if page.Limit < 1 {
		return nil, fmt.Errorf("list imports of user %d: limit %d below 1: %w", userID, page.Limit, store.ErrInvalid)
	}

	if filter.Status != "" && !filter.Status.Known() {
		return nil, fmt.Errorf("list imports of user %d: status %q: %w", userID, filter.Status, store.ErrInvalid)
	}

	// The position before every import, so that the query always has one.
	params := sqlcgen.ListUserImportsParams{
		UserID:     userID,
		Status:     sql.NullString{String: string(filter.Status), Valid: filter.Status != ""},
		AfterAt:    math.MaxInt64,
		AfterID:    math.MaxInt64,
		MaxImports: int64(page.Limit),
	}

	if page.After != (store.Cursor{}) {
		params.AfterAt = toMillis(page.After.At)
		params.AfterID = page.After.ID
	}

	rows, err := r.q.read().ListUserImports(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list imports of user %d: %w", userID, err)
	}

	listed := make([]store.Import, 0, len(rows))

	for _, row := range rows {
		imp, err := importOf(row)
		if err != nil {
			return nil, fmt.Errorf("list imports of user %d: %w", userID, err)
		}

		listed = append(listed, imp)
	}

	return listed, nil
}

func (r imports) SetStatus(ctx context.Context, userID, id int64, status store.ImportStatus) (store.Import, error) {
	if status != store.ImportRunning && !status.Finished() {
		return store.Import{}, fmt.Errorf("set import %d of user %d %q: %w", id, userID, status, store.ErrInvalid)
	}

	var row sqlcgen.Import

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		now := nullMillis(r.clk.Now())

		var err error

		if status == store.ImportRunning {
			row, err = q.StartUserImport(ctx, sqlcgen.StartUserImportParams{StartedAt: now, UserID: userID, ID: id})
		} else {
			row, err = q.FinishUserImport(ctx, sqlcgen.FinishUserImportParams{
				Status:     string(status),
				FinishedAt: now,
				UserID:     userID,
				ID:         id,
			})
		}

		if errors.Is(err, sql.ErrNoRows) {
			return missingOrConflict(ctx, q, userID, id)
		}

		return err
	})
	if err != nil {
		return store.Import{}, fmt.Errorf("set import %d of user %d %q: %w", id, userID, status, err)
	}

	return importOf(row)
}

func (r imports) SaveProgress(ctx context.Context, userID, id int64, progress store.ImportProgress) error {
	if progress.Position < 0 {
		return fmt.Errorf("save progress of import %d of user %d: position %d: %w", id, userID, progress.Position, store.ErrInvalid)
	}

	columns, err := encodeReport(progress.Report)
	if err != nil {
		return fmt.Errorf("save progress of import %d of user %d: %w", id, userID, err)
	}

	err = r.q.write(ctx, func(q *sqlcgen.Queries) error {
		saved, err := q.SaveUserImportProgress(ctx, sqlcgen.SaveUserImportProgressParams{
			Position:     progress.Position,
			Counters:     columns.counters,
			TypeCounters: columns.typeCounters,
			Errors:       columns.errors,
			UserID:       userID,
			ID:           id,
		})
		if err != nil {
			return err
		}

		if saved == 0 {
			return missingOrConflict(ctx, q, userID, id)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("save progress of import %d of user %d: %w", id, userID, err)
	}

	return nil
}

func (r imports) SetClient(ctx context.Context, userID, id int64, client map[string]string) error {
	encoded, err := encodeClient(client)
	if err != nil {
		return fmt.Errorf("set client of import %d of user %d: %w", id, userID, err)
	}

	err = r.q.write(ctx, func(q *sqlcgen.Queries) error {
		set, err := q.SetUserImportClient(ctx, sqlcgen.SetUserImportClientParams{Client: encoded, UserID: userID, ID: id})
		if err != nil {
			return err
		}

		if set == 0 {
			return missingOrConflict(ctx, q, userID, id)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("set client of import %d of user %d: %w", id, userID, err)
	}

	return nil
}

func (r imports) InboxFilesKept(ctx context.Context, failedSince time.Time) ([]string, error) {
	files, err := r.q.read().InboxFilesKept(ctx, nullMillis(failedSince))
	if err != nil {
		return nil, fmt.Errorf("list kept inbox files: %w", err)
	}

	return files, nil
}

func (r imports) LastByOrigin(ctx context.Context, userIDs []int64) (map[int64]map[store.Origin]time.Time, error) {
	last := make(map[int64]map[store.Origin]time.Time, len(userIDs))
	if len(userIDs) == 0 {
		return last, nil
	}

	rows, err := r.q.read().LastCompletedUsersImports(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("last imports of %d users: %w", len(userIDs), err)
	}

	for _, row := range rows {
		if last[row.UserID] == nil {
			last[row.UserID] = map[store.Origin]time.Time{}
		}

		last[row.UserID][store.Origin(row.Origin)] = fromMillis(row.FinishedAt)
	}

	return last, nil
}

func (r imports) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete imports of user %d: limit %d below 1: %w", userID, limit, store.ErrInvalid)
	}

	var deleted int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		deleted, err = q.DeleteUserImports(ctx, sqlcgen.DeleteUserImportsParams{UserID: userID, BatchSize: int64(limit)})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete imports of user %d: %w", userID, err)
	}

	return deleted, nil
}

// missingOrConflict returns the error of a write that changed no import of id of userID:
// store.ErrNotFound when the user has no such import, and store.ErrConflict when its status refused
// the write.
func missingOrConflict(ctx context.Context, q *sqlcgen.Queries, userID, id int64) error {
	_, err := q.GetUserImport(ctx, sqlcgen.GetUserImportParams{UserID: userID, ID: id})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return store.ErrNotFound
	case err != nil:
		return err
	default:
		return store.ErrConflict
	}
}

// importCounters is the JSON of the counters of FR-3 of Ingestion pipeline, a counter at zero left
// out.
type importCounters struct {
	Received    int64 `json:"received,omitzero"`
	Inserted    int64 `json:"inserted,omitzero"`
	Updated     int64 `json:"updated,omitzero"`
	Duplicates  int64 `json:"duplicates,omitzero"`
	Blocked     int64 `json:"blocked,omitzero"`
	Invalid     int64 `json:"invalid,omitzero"`
	Unsupported int64 `json:"unsupported,omitzero"`
	Suspicious  int64 `json:"suspicious,omitzero"`
	Warnings    int64 `json:"warnings,omitzero"`
	Deleted     int64 `json:"deleted,omitzero"`
	NotFound    int64 `json:"notFound,omitzero"`
}

// typeCounters is the JSON of the type_counters column: the counters of each type key, and those of
// the workouts, which are counted apart from the types.
type typeCounters struct {
	Types    map[string]importCounters `json:"types,omitempty"`
	Workouts importCounters            `json:"workouts,omitzero"`
}

// reportColumns are the JSON columns that hold the report of an import.
type reportColumns struct {
	counters     string
	typeCounters string
	errors       string
}

// encodeReport returns the columns of report, keeping its first store.ImportErrorsLimit error
// messages.
func encodeReport(report store.ImportReport) (reportColumns, error) {
	counters, err := json.Marshal(importCounters(report.Total))
	if err != nil {
		return reportColumns{}, fmt.Errorf("encode counters: %w", err)
	}

	perType := typeCounters{Workouts: importCounters(report.Workouts)}
	if len(report.Types) > 0 {
		perType.Types = make(map[string]importCounters, len(report.Types))
		for key, c := range report.Types {
			perType.Types[key] = importCounters(c)
		}
	}

	types, err := json.Marshal(perType)
	if err != nil {
		return reportColumns{}, fmt.Errorf("encode type counters: %w", err)
	}

	messages := report.Errors[:min(len(report.Errors), store.ImportErrorsLimit)]
	if messages == nil {
		messages = []string{}
	}

	errs, err := json.Marshal(messages)
	if err != nil {
		return reportColumns{}, fmt.Errorf("encode errors: %w", err)
	}

	return reportColumns{counters: string(counters), typeCounters: string(types), errors: string(errs)}, nil
}

// decodeReport returns the report of the counters, type_counters and errors columns of row, its
// empty types and errors nil.
func decodeReport(row sqlcgen.Import) (store.ImportReport, error) {
	var (
		total    importCounters
		perType  typeCounters
		messages []string
	)

	if err := json.Unmarshal([]byte(row.Counters), &total); err != nil {
		return store.ImportReport{}, fmt.Errorf("decode counters: %w", err)
	}

	if err := json.Unmarshal([]byte(row.TypeCounters), &perType); err != nil {
		return store.ImportReport{}, fmt.Errorf("decode type counters: %w", err)
	}

	if err := json.Unmarshal([]byte(row.Errors), &messages); err != nil {
		return store.ImportReport{}, fmt.Errorf("decode errors: %w", err)
	}

	report := store.ImportReport{Total: store.ImportCounters(total), Workouts: store.ImportCounters(perType.Workouts)}

	if len(perType.Types) > 0 {
		report.Types = make(map[string]store.ImportCounters, len(perType.Types))
		for key, c := range perType.Types {
			report.Types[key] = store.ImportCounters(c)
		}
	}

	if len(messages) > 0 {
		report.Errors = messages
	}

	return report, nil
}

// encodeClient returns the client column of client, NULL when it is empty.
func encodeClient(client map[string]string) (sql.NullString, error) {
	if len(client) == 0 {
		return sql.NullString{}, nil
	}

	data, err := json.Marshal(client)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode client: %w", err)
	}

	return sql.NullString{String: string(data), Valid: true}, nil
}

// decodeClient returns the client of the client column, nil when it is NULL.
func decodeClient(column sql.NullString) (map[string]string, error) {
	if !column.Valid {
		return nil, nil
	}

	var client map[string]string
	if err := json.Unmarshal([]byte(column.String), &client); err != nil {
		return nil, fmt.Errorf("decode client: %w", err)
	}

	return client, nil
}

func importOf(row sqlcgen.Import) (store.Import, error) {
	client, err := decodeClient(row.Client)
	if err != nil {
		return store.Import{}, fmt.Errorf("import %d: %w", row.ID, err)
	}

	report, err := decodeReport(row)
	if err != nil {
		return store.Import{}, fmt.Errorf("import %d: %w", row.ID, err)
	}

	return store.Import{
		ID:         row.ID,
		UserID:     row.UserID,
		Origin:     store.Origin(row.Origin),
		TokenID:    row.TokenID.Int64,
		Client:     client,
		InboxFile:  row.InboxFile.String,
		Status:     store.ImportStatus(row.Status),
		Position:   row.Position,
		Report:     report,
		CreatedAt:  fromMillis(row.CreatedAt),
		StartedAt:  fromNullMillis(row.StartedAt),
		FinishedAt: fromNullMillis(row.FinishedAt),
	}, nil
}
