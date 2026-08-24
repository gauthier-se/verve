package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Exclusion is a standing refusal that a stretch of a Metric is Verve's to hold
// (CONTEXT.md): no Connector may write what it names, and what was already stored
// under it was deleted when it was created. StartsOn and EndsOn are inclusive
// YYYY-MM-DD bounds, empty for unbounded, so an Exclusion with neither covers the
// whole of that Metric. Purged is what the purge removed, kept as the historical
// figure the History reads and never recomputed.
type Exclusion struct {
	ID        int64
	AccountID int64
	Metric    string
	StartsOn  string
	EndsOn    string
	Purged    int64
	CreatedAt string
}

// ExclusionModel is the DAO for exclusions.
type ExclusionModel struct {
	DB *sql.DB
}

const exclusionColumns = `id, account_id, metric, starts_on, ends_on, purged, created_at`

// ListByAccount returns the Account's whole set, ordered by Metric then span. It is
// read once per import and once per page: the set is small by nature, and there is
// no windowed read because an Exclusion is not drawn on a time axis.
func (m ExclusionModel) ListByAccount(ctx context.Context, accountID int64) ([]Exclusion, error) {
	const query = `
		SELECT ` + exclusionColumns + `
		FROM exclusions
		WHERE account_id = ?
		ORDER BY metric, starts_on, id`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list exclusions: %w", err)
	}
	defer rows.Close()

	exclusions := []Exclusion{}
	for rows.Next() {
		var e Exclusion
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Metric, &e.StartsOn, &e.EndsOn,
			&e.Purged, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("data: scan exclusion: %w", err)
		}
		exclusions = append(exclusions, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate exclusions: %w", err)
	}
	return exclusions, nil
}

// Insert writes an Exclusion and purges what it covers, in one transaction, and
// populates e with the resulting row including how many Measurements went.
//
// The two halves never separate, because each alone fails invisibly: a rule with no
// purge is a delete that did not happen, and a purge with no rule is data the next
// import restores (ADR 0022, ADR 0033). The transaction is what makes "this Metric
// is not mine to hold" a single fact rather than two hopeful ones.
//
// Saying the same thing twice is not an error: on a duplicate the existing row is
// returned with its original purge count and false, the same idempotence InsertOne
// gives a re-typed Measurement. Re-reporting the first purge would tell an owner
// that 431 more readings just went.
func (m ExclusionModel) Insert(ctx context.Context, e *Exclusion) (bool, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("data: begin exclusion: %w", err)
	}
	defer tx.Rollback() // no-op after a successful Commit

	const insert = `
		INSERT INTO exclusions (account_id, metric, starts_on, ends_on)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (account_id, metric, starts_on, ends_on) DO NOTHING
		RETURNING id, created_at`
	err = tx.QueryRowContext(ctx, insert, e.AccountID, e.Metric, e.StartsOn, e.EndsOn).
		Scan(&e.ID, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// DO NOTHING suppressed the insert, so RETURNING yielded no row: this
		// Exclusion already stands, and its purge already ran.
		const existing = `
			SELECT ` + exclusionColumns + `
			FROM exclusions
			WHERE account_id = ? AND metric = ? AND starts_on = ? AND ends_on = ?`
		if err := tx.QueryRowContext(ctx, existing, e.AccountID, e.Metric, e.StartsOn, e.EndsOn).
			Scan(&e.ID, &e.AccountID, &e.Metric, &e.StartsOn, &e.EndsOn, &e.Purged, &e.CreatedAt); err != nil {
			return false, fmt.Errorf("data: resolve existing exclusion: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("data: insert exclusion: %w", err)
	}

	purged, err := deleteMeasurementsInSpan(ctx, tx, e.AccountID, e.Metric, e.StartsOn, e.EndsOn)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE exclusions SET purged = ? WHERE id = ?`, purged, e.ID); err != nil {
		return false, fmt.Errorf("data: record purge count: %w", err)
	}
	e.Purged = purged

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("data: commit exclusion: %w", err)
	}
	return true, nil
}

// Delete removes the Account's Exclusion, scoped by Account so one Account can never
// touch another's row. ErrRecordNotFound if there is no such row.
//
// It deletes the rule and nothing else. Nothing is restored, and nothing can be: the
// rows are gone, and what brings the imported ones back is the next import, which
// this deletion has just stopped refusing.
func (m ExclusionModel) Delete(ctx context.Context, accountID, id int64) error {
	return execExpectingRow(ctx, m.DB,
		`DELETE FROM exclusions WHERE id = ? AND account_id = ?`, id, accountID)
}

// ExclusionSet is an Account's Exclusions indexed by Metric slug: the form a
// Connector reads them in, built once at the start of an import and asked about
// every Record. The zero value excludes nothing, which is every Account that has
// never excluded anything, and its lookup misses on the first map probe.
type ExclusionSet map[string][]Exclusion

// Set loads the Account's Exclusions indexed for an import.
func (m ExclusionModel) Set(ctx context.Context, accountID int64) (ExclusionSet, error) {
	rows, err := m.ListByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	set := make(ExclusionSet, len(rows))
	for _, e := range rows {
		set[e.Metric] = append(set[e.Metric], e)
	}
	return set, nil
}

// Excludes reports whether this Account refused metric on the day startAt falls in.
// startAt is the stored RFC 3339 UTC timestamp, so the day is its first ten
// characters and the comparison is lexical: a zero-padded YYYY-MM-DD prefix orders
// exactly as a date does, which is why measurements are normalized to that format.
//
// This is the Go twin of measurementSpanFilter, and the two are pinned to each other
// by test. They have to answer identically or the feature lies in one direction or
// the other: a purge that removes a day the next import re-admits, or an import that
// refuses a day the purge left standing.
func (s ExclusionSet) Excludes(metric, startAt string) bool {
	spans, ok := s[metric]
	if !ok {
		return false
	}

	// A Record can arrive with a timestamp no day can be read from: normalizeTime
	// keeps an unparseable Apple value verbatim rather than dropping it. SQLite's
	// date() yields NULL for such a value, and every comparison against NULL is
	// NULL, so the purge leaves the row alone. A bounded span here must do the
	// same, or the import refuses a day the purge left standing.
	var day string
	readable := false
	if len(startAt) >= dayWidth {
		day = startAt[:dayWidth]
		_, err := time.Parse(dayLayout, day)
		readable = err == nil
	}

	for _, e := range spans {
		if e.StartsOn == "" && e.EndsOn == "" {
			// Unbounded: the whole Metric, with no comparison to make, which is what
			// the purge's `'' = ''` short-circuit does to an unreadable row too.
			return true
		}
		if !readable {
			continue
		}
		if e.StartsOn != "" && day < e.StartsOn {
			continue
		}
		if e.EndsOn != "" && day > e.EndsOn {
			continue
		}
		return true
	}
	return false
}

// dayWidth is the width of a YYYY-MM-DD day, which is the prefix every stored
// RFC 3339 timestamp opens with. Computed from the layout it measures.
const dayWidth = len(dayLayout)

// dayLayout is the day a bound is spelled in, and the prefix of a stored timestamp.
const dayLayout = "2006-01-02"
