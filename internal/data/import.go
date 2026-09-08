package data

import (
	"context"
	"database/sql"
	"fmt"
)

// UnmappedRecord is one incoming row the Catalog could not map, kept verbatim in
// the Unmapped bin rather than discarded (CONTEXT.md: Unmapped bin, ADR 0002).
// Value and Unit are the source's own strings, uninterpreted.
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

// Import is the persisted record of one finished Connector run over one export:
// what read it, what it read, and what it did (CONTEXT.md: Import). Every run is
// kept, including the ones that added nothing — that is what proves idempotence
// (ADR 0006).
type Import struct {
	ID            int64
	AccountID     int64
	Connector     string // the Connector that ran it, e.g. "applehealth"
	SourceFile    string
	AddedCount    int
	SkippedCount  int
	UnmappedCount int
	ImportedAt    string
}

// ImportModel is the DAO for the two tables an Import owns: the run records and
// the Unmapped bin. Neither is a measurement, which is the whole reason they are
// not on MeasurementModel: a model named after a table it does not read is how
// "where does this query live" stops having an answer.
type ImportModel struct {
	DB *sql.DB
}

// Record writes the summary row for one Import run and populates its generated ID
// and timestamp.
func (m ImportModel) Record(ctx context.Context, imp *Import) error {
	const query = `
		INSERT INTO imports (account_id, connector, source_file, added_count, skipped_count, unmapped_count)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, imported_at`
	return m.DB.QueryRowContext(ctx, query,
		imp.AccountID, imp.Connector, imp.SourceFile, imp.AddedCount, imp.SkippedCount, imp.UnmappedCount,
	).Scan(&imp.ID, &imp.ImportedAt)
}

// List returns the Account's recorded Import runs, most recent first.
func (m ImportModel) List(ctx context.Context, accountID int64) ([]Import, error) {
	const query = `
		SELECT id, account_id, connector, source_file, added_count, skipped_count, unmapped_count, imported_at
		FROM imports WHERE account_id = ?
		ORDER BY imported_at DESC, id DESC`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: import List: %w", err)
	}
	defer rows.Close()

	out := []Import{}
	for rows.Next() {
		var imp Import
		if err := rows.Scan(&imp.ID, &imp.AccountID, &imp.Connector, &imp.SourceFile,
			&imp.AddedCount, &imp.SkippedCount, &imp.UnmappedCount, &imp.ImportedAt); err != nil {
			return nil, fmt.Errorf("data: import List scan: %w", err)
		}
		out = append(out, imp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: import List: %w", err)
	}
	return out, nil
}

// InsertUnmappedBatch inserts Unmapped records in one transaction, deduped by
// content key like measurements; returns a mask (inserted[i] true iff newly kept).
func (m ImportModel) InsertUnmappedBatch(ctx context.Context, us []UnmappedRecord) ([]bool, error) {
	inserted := make([]bool, len(us))
	if len(us) == 0 {
		return inserted, nil
	}

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("data: begin unmapped batch: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

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

// CountUnmapped is how many records the Catalog could not map and the bin kept
// (ADR 0002). It is read next to the imports because it is the other half of the
// same promise: nothing incoming is discarded, even when nothing can read it yet.
func (m ImportModel) CountUnmapped(ctx context.Context, accountID int64) (int, error) {
	const query = `SELECT COUNT(*) FROM unmapped_records WHERE account_id = ?`
	var n int
	if err := m.DB.QueryRowContext(ctx, query, accountID).Scan(&n); err != nil {
		return 0, fmt.Errorf("data: import CountUnmapped: %w", err)
	}
	return n, nil
}
