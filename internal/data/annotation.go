package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Annotation is dated context the Account wrote about a day or a span of days: an
// illness, a trip, a change of program (CONTEXT.md). It carries no value, no
// unit and no Metric: it is not a Measurement, and the Metric it is read against
// is whichever Panel happens to be on screen. EndsOn is nil for a single day.
type Annotation struct {
	ID        int64
	AccountID int64
	Label     string
	Body      *string
	StartsOn  string
	EndsOn    *string
	CreatedAt string
	UpdatedAt string
}

// AnnotationModel is the DAO for annotations.
type AnnotationModel struct {
	DB *sql.DB
}

const annotationColumns = `id, account_id, label, body, starts_on, ends_on, created_at, updated_at`

// ListByWindow returns the Account's Annotations overlapping the half-open day
// window [from, to), in chronological order.
//
// The predicate is an overlap, not a containment: a span that began before the
// window and is still running belongs on the chart, and "flu, 12-19 March" read
// on a range starting the 15th is exactly the case a filter on starts_on alone
// would drop. Bounds are YYYY-MM-DD, so lexical comparison is chronological.
func (m AnnotationModel) ListByWindow(ctx context.Context, accountID int64, from, to string) ([]Annotation, error) {
	const query = `
		SELECT ` + annotationColumns + `
		FROM annotations
		WHERE account_id = ?
		  AND starts_on < ?
		  AND COALESCE(ends_on, starts_on) >= ?
		ORDER BY starts_on, id`
	rows, err := m.DB.QueryContext(ctx, query, accountID, to, from)
	if err != nil {
		return nil, fmt.Errorf("data: list annotations: %w", err)
	}
	defer rows.Close()

	return scanAnnotations(rows)
}

// ListAll returns every Annotation of the Account, most recent span first. It backs
// the Data page's list, which is the only view that reaches an Annotation outside
// the current range.
func (m AnnotationModel) ListAll(ctx context.Context, accountID int64) ([]Annotation, error) {
	const query = `
		SELECT ` + annotationColumns + `
		FROM annotations
		WHERE account_id = ?
		ORDER BY starts_on DESC, id DESC`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list all annotations: %w", err)
	}
	defer rows.Close()

	return scanAnnotations(rows)
}

// GetByID returns the Account's Annotation, or ErrRecordNotFound (also for another
// Account's id, so a probe cannot tell missing from forbidden).
func (m AnnotationModel) GetByID(ctx context.Context, accountID, id int64) (*Annotation, error) {
	const query = `SELECT ` + annotationColumns + ` FROM annotations WHERE id = ? AND account_id = ?`
	var a Annotation
	err := m.DB.QueryRowContext(ctx, query, id, accountID).Scan(&a.ID, &a.AccountID, &a.Label,
		&a.Body, &a.StartsOn, &a.EndsOn, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("data: get annotation %d: %w", id, err)
	}
	return &a, nil
}

// Insert writes a new Annotation and populates ID and timestamps.
func (m AnnotationModel) Insert(ctx context.Context, a *Annotation) error {
	const query = `
		INSERT INTO annotations (account_id, label, body, starts_on, ends_on)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`
	err := m.DB.QueryRowContext(ctx, query, a.AccountID, a.Label, a.Body, a.StartsOn, a.EndsOn).
		Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("data: insert annotation: %w", err)
	}
	return nil
}

// Update saves an Annotation's label, body and span, scoped to a.AccountID;
// ErrRecordNotFound if none belongs to the Account.
func (m AnnotationModel) Update(ctx context.Context, a *Annotation) error {
	const query = `
		UPDATE annotations
		SET label = ?, body = ?, starts_on = ?, ends_on = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND account_id = ?`
	return execExpectingRow(ctx, m.DB, query, a.Label, a.Body, a.StartsOn, a.EndsOn, a.ID, a.AccountID)
}

// Delete removes the Account's Annotation, scoped by Account, so one Account can
// never touch another's row. ErrRecordNotFound if there is no such row.
func (m AnnotationModel) Delete(ctx context.Context, accountID, id int64) error {
	return execExpectingRow(ctx, m.DB,
		`DELETE FROM annotations WHERE id = ? AND account_id = ?`, id, accountID)
}

// scanAnnotations drains a rows cursor selecting annotationColumns.
func scanAnnotations(rows *sql.Rows) ([]Annotation, error) {
	annotations := []Annotation{}
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.AccountID, &a.Label, &a.Body,
			&a.StartsOn, &a.EndsOn, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("data: scan annotation: %w", err)
		}
		annotations = append(annotations, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate annotations: %w", err)
	}
	return annotations, nil
}
