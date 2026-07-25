package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/catalog"
)

// Measurement is a scalar value of a canonical Metric over a point in time, owned
// by one Account. Value is normalized to the Metric's unit; OriginalUnit is what
// the Source reported. ContentKey is the dedup identity (ADR 0006).
//
// ID is populated by InsertOne and by reads that need to address a single row
// (ListManual). The bulk import path leaves it zero: it writes hundreds of
// thousands of rows and nothing downstream addresses them individually.
type Measurement struct {
	ID           int64
	AccountID    int64
	Metric       string
	Value        float64
	OriginalUnit string
	StartAt      string
	EndAt        string
	Source       string
	ContentKey   string
}

// UnmappedRecord is an incoming record the Connector could not map, kept in the
// Unmapped bin (ADR 0002); Value is raw source text, possibly non-numeric.
type UnmappedRecord struct {
	AccountID  int64
	SourceType string
	Value      string
	Unit       string
	StartAt    string
	EndAt      string
	Source     string
	ContentKey string
}

// Import is one recorded run of a Connector over a Source file.
type Import struct {
	ID            int64
	AccountID     int64
	SourceFile    string
	AddedCount    int
	SkippedCount  int
	UnmappedCount int
	ImportedAt    string
}

// MeasurementModel is the DAO for measurements and the Unmapped bin.
type MeasurementModel struct {
	DB *sql.DB
}

// InsertBatch inserts Measurements in one transaction, skipping existing
// (account, content_key) so re-import is idempotent (ADR 0006). Returns a mask
// parallel to ms: inserted[i] is true iff ms[i] was new. Batching bounds memory
// and the WAL during a large import.
func (m MeasurementModel) InsertBatch(ctx context.Context, ms []Measurement) ([]bool, error) {
	inserted := make([]bool, len(ms))
	if len(ms) == 0 {
		return inserted, nil
	}

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("data: begin measurement batch: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	const query = `
		INSERT OR IGNORE INTO measurements
			(account_id, metric, value, original_unit, start_at, end_at, source, content_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("data: prepare measurement insert: %w", err)
	}
	defer stmt.Close()

	for i, row := range ms {
		res, err := stmt.ExecContext(ctx,
			row.AccountID, row.Metric, row.Value, row.OriginalUnit,
			row.StartAt, row.EndAt, row.Source, row.ContentKey)
		if err != nil {
			return nil, fmt.Errorf("data: insert measurement: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("data: measurement rows affected: %w", err)
		}
		inserted[i] = n == 1
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("data: commit measurement batch: %w", err)
	}
	return inserted, nil
}

// InsertOne inserts a single Measurement — the Manual entry path (ADR 0022) — and
// populates its ID. Unlike InsertBatch it must tell the caller *which* row it landed
// on, so an HTTP client can address and later delete it.
//
// A content-key collision is not an error: it means this exact value at this exact
// time is already stored, so the existing row's id is returned and the write is a
// no-op. That is the same idempotence a re-import gets (ADR 0006) — typing a value
// twice should be as harmless as importing the same export twice.
func (m MeasurementModel) InsertOne(ctx context.Context, row *Measurement) error {
	const insert = `
		INSERT INTO measurements
			(account_id, metric, value, original_unit, start_at, end_at, source, content_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, content_key) DO NOTHING
		RETURNING id`

	err := m.DB.QueryRowContext(ctx, insert,
		row.AccountID, row.Metric, row.Value, row.OriginalUnit,
		row.StartAt, row.EndAt, row.Source, row.ContentKey,
	).Scan(&row.ID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("data: insert measurement: %w", err)
	}

	// DO NOTHING suppressed the insert, so RETURNING yielded no row: the content
	// key already exists. Resolve the existing id rather than failing.
	const existing = `SELECT id FROM measurements WHERE account_id = ? AND content_key = ?`
	if err := m.DB.QueryRowContext(ctx, existing, row.AccountID, row.ContentKey).Scan(&row.ID); err != nil {
		return fmt.Errorf("data: resolve existing measurement: %w", err)
	}
	return nil
}

// Delete removes one Manual entry. The `source` predicate is part of the statement,
// not a check in the caller, so no future call site can delete imported data however
// it invokes this: removing an imported row would drop its content key and the next
// Import would silently restore it (ADR 0022). Returns ErrRecordNotFound when no row
// matches — absent, owned by another Account, or not a Manual entry.
func (m MeasurementModel) Delete(ctx context.Context, accountID, id int64) error {
	const query = `DELETE FROM measurements WHERE id = ? AND account_id = ? AND source = ?`
	res, err := m.DB.ExecContext(ctx, query, id, accountID, catalog.SourceManual)
	if err != nil {
		return fmt.Errorf("data: delete measurement: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("data: delete measurement rows affected: %w", err)
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// GetByID returns one Measurement of the Account, or ErrRecordNotFound. The delete
// handler reads the row first so it can tell "not yours / absent" (404) from "yours
// but imported" (403) — a distinction Delete alone cannot express, since both cases
// simply match no row.
func (m MeasurementModel) GetByID(ctx context.Context, accountID, id int64) (*Measurement, error) {
	const query = `
		SELECT id, account_id, metric, value, original_unit, start_at, end_at, source, content_key
		FROM measurements
		WHERE id = ? AND account_id = ?`
	var row Measurement
	err := m.DB.QueryRowContext(ctx, query, id, accountID).Scan(
		&row.ID, &row.AccountID, &row.Metric, &row.Value, &row.OriginalUnit,
		&row.StartAt, &row.EndAt, &row.Source, &row.ContentKey,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("data: get measurement: %w", err)
	}
	return &row, nil
}

// ListManual returns the Account's Manual entries, newest first, optionally for one
// Metric. It exists because the Ledger serves aggregated Series rows (ADR 0021),
// which carry no row id — and an entry that cannot be addressed cannot be deleted.
func (m MeasurementModel) ListManual(ctx context.Context, accountID int64, metric string, limit int) ([]Measurement, error) {
	const query = `
		SELECT id, account_id, metric, value, original_unit, start_at, end_at, source, content_key
		FROM measurements
		WHERE account_id = ? AND source = ? AND (? = '' OR metric = ?)
		ORDER BY start_at DESC, id DESC
		LIMIT ?`

	rows, err := m.DB.QueryContext(ctx, query, accountID, catalog.SourceManual, metric, metric, limit)
	if err != nil {
		return nil, fmt.Errorf("data: list manual measurements: %w", err)
	}
	defer rows.Close()

	out := []Measurement{}
	for rows.Next() {
		var row Measurement
		if err := rows.Scan(
			&row.ID, &row.AccountID, &row.Metric, &row.Value, &row.OriginalUnit,
			&row.StartAt, &row.EndAt, &row.Source, &row.ContentKey,
		); err != nil {
			return nil, fmt.Errorf("data: scan manual measurement: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate manual measurements: %w", err)
	}
	return out, nil
}

// InsertUnmappedBatch inserts Unmapped records in one transaction, deduped by
// content key like measurements; returns a mask (inserted[i] true iff newly kept).
func (m MeasurementModel) InsertUnmappedBatch(ctx context.Context, us []UnmappedRecord) ([]bool, error) {
	inserted := make([]bool, len(us))
	if len(us) == 0 {
		return inserted, nil
	}

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("data: begin unmapped batch: %w", err)
	}
	defer tx.Rollback()

	const query = `
		INSERT OR IGNORE INTO unmapped_records
			(account_id, source_type, value, unit, start_at, end_at, source, content_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("data: prepare unmapped insert: %w", err)
	}
	defer stmt.Close()

	for i, row := range us {
		res, err := stmt.ExecContext(ctx,
			row.AccountID, row.SourceType, row.Value, row.Unit,
			row.StartAt, row.EndAt, row.Source, row.ContentKey)
		if err != nil {
			return nil, fmt.Errorf("data: insert unmapped: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("data: unmapped rows affected: %w", err)
		}
		inserted[i] = n == 1
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("data: commit unmapped batch: %w", err)
	}
	return inserted, nil
}

// HasAny reports whether accountID has any Measurement — the signal the web
// empty-state uses to decide between "import your data" and the filled Panels
// (ADR 0016, ADR 0018).
func (m MeasurementModel) HasAny(ctx context.Context, accountID int64) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM measurements WHERE account_id = ?)`
	var exists bool
	if err := m.DB.QueryRowContext(ctx, query, accountID).Scan(&exists); err != nil {
		return false, fmt.Errorf("data: measurement HasAny: %w", err)
	}
	return exists, nil
}

// DistinctMetrics lists the Catalog slugs the Account has at least one Measurement
// for, sorted for a stable listing. It is the row set of the Ledger overview
// (ADR 0021) — only Metrics with data, so the scoreboard shows no empty rows. Uses
// the measurements_account_metric_start index.
func (m MeasurementModel) DistinctMetrics(ctx context.Context, accountID int64) ([]string, error) {
	const query = `SELECT DISTINCT metric FROM measurements WHERE account_id = ? ORDER BY metric`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: measurement DistinctMetrics: %w", err)
	}
	defer rows.Close()

	var metrics []string
	for rows.Next() {
		var metric string
		if err := rows.Scan(&metric); err != nil {
			return nil, fmt.Errorf("data: measurement DistinctMetrics scan: %w", err)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: measurement DistinctMetrics: %w", err)
	}
	return metrics, nil
}

// RecordImport writes the summary row for one Import run and populates its
// generated ID and timestamp.
func (m MeasurementModel) RecordImport(ctx context.Context, imp *Import) error {
	const query = `
		INSERT INTO imports (account_id, source_file, added_count, skipped_count, unmapped_count)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, imported_at`
	return m.DB.QueryRowContext(ctx, query,
		imp.AccountID, imp.SourceFile, imp.AddedCount, imp.SkippedCount, imp.UnmappedCount,
	).Scan(&imp.ID, &imp.ImportedAt)
}
