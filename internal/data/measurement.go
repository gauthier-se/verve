package data

import (
	"context"
	"database/sql"
	"fmt"
)

// Measurement is a scalar value of a canonical Metric over a point in time, owned
// by one Account. Value is normalized to the Metric's unit; OriginalUnit is what
// the Source reported. ContentKey is the dedup identity (ADR 0006).
type Measurement struct {
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
