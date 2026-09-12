package data

import (
	"context"
	"fmt"
)

// Taking a whole Account out of the store, one page at a time: the reads behind
// an Archive (ADR 0039). They are here rather than in internal/archive because
// they are SQL over the families, and the format has no business writing SQL.
//
// Every family is ordered by the prefix of its own index and tie-broken by
// content_key, which UNIQUE (account_id, content_key) makes distinct. Two
// properties follow, and the export needs both: the order is total, so paging
// can neither repeat nor skip a row, and it is reproducible, so two exports of
// the same data are byte-identical. Ordering by start_at across a whole Account
// would read better in a spec and would make SQLite materialize and sort
// millions of rows on the machine least able to afford it.
//
// A cursor is the ordering tuple of the last row of the previous page. Its zero
// value starts at the beginning: every stored component is non-empty, so the
// empty tuple sorts before all of them and one predicate serves the first page
// and every page after it.

// MeasurementCursor addresses a page boundary in (metric, start_at, content_key).
type MeasurementCursor struct {
	Metric     string
	StartAt    string
	ContentKey string
}

// ExportPage returns the Account's Measurements after the cursor, in export
// order, with the cursor to pass back for the next page. A short page is the
// last one; the caller stops on an empty one.
func (m MeasurementModel) ExportPage(ctx context.Context, accountID int64, after MeasurementCursor, limit int) ([]Measurement, MeasurementCursor, error) {
	const query = `
		SELECT metric, value, original_unit, start_at, end_at, source, content_key
		  FROM measurements
		 WHERE account_id = ?
		   AND (metric, start_at, content_key) > (?, ?, ?)
		 ORDER BY metric, start_at, content_key
		 LIMIT ?`
	rows, err := m.DB.QueryContext(ctx, query, accountID, after.Metric, after.StartAt, after.ContentKey, limit)
	if err != nil {
		return nil, after, fmt.Errorf("data: export measurements: %w", err)
	}
	defer rows.Close()

	out := make([]Measurement, 0, limit)
	for rows.Next() {
		r := Measurement{AccountID: accountID}
		if err := rows.Scan(&r.Metric, &r.Value, &r.OriginalUnit, &r.StartAt, &r.EndAt, &r.Source, &r.ContentKey); err != nil {
			return nil, after, fmt.Errorf("data: export measurements scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, after, fmt.Errorf("data: export measurements: %w", err)
	}
	if n := len(out); n > 0 {
		last := out[n-1]
		after = MeasurementCursor{Metric: last.Metric, StartAt: last.StartAt, ContentKey: last.ContentKey}
	}
	return out, after, nil
}

// StateCursor addresses a page boundary in (kind, start_at, content_key).
type StateCursor struct {
	Kind       string
	StartAt    string
	ContentKey string
}

// ExportPage returns the Account's States after the cursor, in export order.
func (m StateModel) ExportPage(ctx context.Context, accountID int64, after StateCursor, limit int) ([]State, StateCursor, error) {
	const query = `
		SELECT kind, state_value, start_at, end_at, source, content_key
		  FROM states
		 WHERE account_id = ?
		   AND (kind, start_at, content_key) > (?, ?, ?)
		 ORDER BY kind, start_at, content_key
		 LIMIT ?`
	rows, err := m.DB.QueryContext(ctx, query, accountID, after.Kind, after.StartAt, after.ContentKey, limit)
	if err != nil {
		return nil, after, fmt.Errorf("data: export states: %w", err)
	}
	defer rows.Close()

	out := make([]State, 0, limit)
	for rows.Next() {
		r := State{AccountID: accountID}
		if err := rows.Scan(&r.Kind, &r.StateValue, &r.StartAt, &r.EndAt, &r.Source, &r.ContentKey); err != nil {
			return nil, after, fmt.Errorf("data: export states scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, after, fmt.Errorf("data: export states: %w", err)
	}
	if n := len(out); n > 0 {
		last := out[n-1]
		after = StateCursor{Kind: last.Kind, StartAt: last.StartAt, ContentKey: last.ContentKey}
	}
	return out, after, nil
}

// UnmappedCursor addresses a page boundary in (source_type, start_at, content_key).
type UnmappedCursor struct {
	SourceType string
	StartAt    string
	ContentKey string
}

// ExportUnmappedPage returns the Account's Unmapped bin after the cursor, in
// export order. The bin travels with the rest: it exists so nothing incoming is
// lost to a Catalog gap (ADR 0002), and an Archive that dropped it would lose
// exactly what Verve promised to keep.
func (m ImportModel) ExportUnmappedPage(ctx context.Context, accountID int64, after UnmappedCursor, limit int) ([]UnmappedRecord, UnmappedCursor, error) {
	const query = `
		SELECT source_type, coalesce(value, ''), coalesce(unit, ''), start_at, end_at, source, content_key
		  FROM unmapped_records
		 WHERE account_id = ?
		   AND (source_type, start_at, content_key) > (?, ?, ?)
		 ORDER BY source_type, start_at, content_key
		 LIMIT ?`
	rows, err := m.DB.QueryContext(ctx, query, accountID, after.SourceType, after.StartAt, after.ContentKey, limit)
	if err != nil {
		return nil, after, fmt.Errorf("data: export unmapped: %w", err)
	}
	defer rows.Close()

	out := make([]UnmappedRecord, 0, limit)
	for rows.Next() {
		r := UnmappedRecord{AccountID: accountID}
		if err := rows.Scan(&r.SourceType, &r.Value, &r.Unit, &r.StartAt, &r.EndAt, &r.Source, &r.ContentKey); err != nil {
			return nil, after, fmt.Errorf("data: export unmapped scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, after, fmt.Errorf("data: export unmapped: %w", err)
	}
	if n := len(out); n > 0 {
		last := out[n-1]
		after = UnmappedCursor{SourceType: last.SourceType, StartAt: last.StartAt, ContentKey: last.ContentKey}
	}
	return out, after, nil
}

// SessionCursor addresses a page boundary in (start_at, content_key).
type SessionCursor struct {
	StartAt    string
	ContentKey string
}

// ExportSession is a workout as it travels: the Session with the stats and
// Routes that belong to it. It is assembled here rather than left to the caller
// because InsertWorkout writes the three as one unit (connector.Store), and a
// Session arriving without its stats is not a workout that lost a column, it is
// a workout that lost its identity as one thing.
type ExportSession struct {
	Session Session
	Stats   []SessionStat
	Routes  []Route
}

// ExportPage returns the Account's Sessions after the cursor, in export order,
// each with its stats and Routes. It runs three queries per page rather than
// three per Session.
func (m SessionModel) ExportPage(ctx context.Context, accountID int64, after SessionCursor, limit int) ([]ExportSession, SessionCursor, error) {
	const query = `
		SELECT id, activity_type, start_at, end_at, duration, total_distance, total_energy, source, content_key
		  FROM sessions
		 WHERE account_id = ?
		   AND (start_at, content_key) > (?, ?)
		 ORDER BY start_at, content_key
		 LIMIT ?`
	rows, err := m.DB.QueryContext(ctx, query, accountID, after.StartAt, after.ContentKey, limit)
	if err != nil {
		return nil, after, fmt.Errorf("data: export sessions: %w", err)
	}

	out := make([]ExportSession, 0, limit)
	index := make(map[int64]int, limit)
	ids := make([]any, 0, limit)
	for rows.Next() {
		s := Session{AccountID: accountID}
		if err := rows.Scan(&s.ID, &s.ActivityType, &s.StartAt, &s.EndAt, &s.Duration,
			&s.TotalDistance, &s.TotalEnergy, &s.Source, &s.ContentKey); err != nil {
			rows.Close()
			return nil, after, fmt.Errorf("data: export sessions scan: %w", err)
		}
		index[s.ID] = len(out)
		ids = append(ids, s.ID)
		out = append(out, ExportSession{Session: s})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, after, fmt.Errorf("data: export sessions: %w", err)
	}
	// Closed before the next statement, not deferred: the pool holds one
	// connection, so an open *sql.Rows across another query deadlocks (see Tx).
	rows.Close()

	if len(out) == 0 {
		return out, after, nil
	}
	if err := m.exportStats(ctx, ids, index, out); err != nil {
		return nil, after, err
	}
	if err := m.exportRoutes(ctx, accountID, ids, index, out); err != nil {
		return nil, after, err
	}

	last := out[len(out)-1].Session
	return out, SessionCursor{StartAt: last.StartAt, ContentKey: last.ContentKey}, nil
}

// exportStats attaches one page of Sessions' stats, ordered so the page is
// reproducible.
func (m SessionModel) exportStats(ctx context.Context, ids []any, index map[int64]int, out []ExportSession) error {
	query := `
		SELECT session_id, metric, stat, value
		  FROM session_stats
		 WHERE session_id IN (` + placeholders(len(ids)) + `)
		 ORDER BY session_id, metric, stat`
	rows, err := m.DB.QueryContext(ctx, query, ids...)
	if err != nil {
		return fmt.Errorf("data: export session stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var st SessionStat
		if err := rows.Scan(&id, &st.Metric, &st.Stat, &st.Value); err != nil {
			return fmt.Errorf("data: export session stats scan: %w", err)
		}
		if i, ok := index[id]; ok {
			out[i].Stats = append(out[i].Stats, st)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("data: export session stats: %w", err)
	}
	return nil
}

// exportRoutes attaches one page of Sessions' Routes, ordered so the page is
// reproducible.
func (m SessionModel) exportRoutes(ctx context.Context, accountID int64, ids []any, index map[int64]int, out []ExportSession) error {
	query := `
		SELECT session_id, artifact, start_at, end_at, source, content_key
		  FROM routes
		 WHERE account_id = ? AND session_id IN (` + placeholders(len(ids)) + `)
		 ORDER BY session_id, start_at, content_key`
	rows, err := m.DB.QueryContext(ctx, query, append([]any{accountID}, ids...)...)
	if err != nil {
		return fmt.Errorf("data: export routes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		r := Route{AccountID: accountID}
		if err := rows.Scan(&r.SessionID, &r.Artifact, &r.StartAt, &r.EndAt, &r.Source, &r.ContentKey); err != nil {
			return fmt.Errorf("data: export routes scan: %w", err)
		}
		if i, ok := index[r.SessionID]; ok {
			out[i].Routes = append(out[i].Routes, r)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("data: export routes: %w", err)
	}
	return nil
}

// ExportImports returns every Import run the Account has recorded, oldest first.
// They travel as provenance and are never replayed: an Archive read back records
// its own single run, like any other Connector. There are a handful of them, so
// there is nothing to page.
func (m ImportModel) ExportImports(ctx context.Context, accountID int64) ([]Import, error) {
	const query = `
		SELECT connector, source_file, added_count, skipped_count, unmapped_count, imported_at
		  FROM imports WHERE account_id = ?
		 ORDER BY imported_at, id`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: export imports: %w", err)
	}
	defer rows.Close()

	out := []Import{}
	for rows.Next() {
		imp := Import{AccountID: accountID}
		if err := rows.Scan(&imp.Connector, &imp.SourceFile, &imp.AddedCount,
			&imp.SkippedCount, &imp.UnmappedCount, &imp.ImportedAt); err != nil {
			return nil, fmt.Errorf("data: export imports scan: %w", err)
		}
		out = append(out, imp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: export imports: %w", err)
	}
	return out, nil
}

// ExportCounts is how many rows each family holds for one Account. It is read
// before the rows are, so an Archive can state up front what it contains and a
// reader can check what it received against it.
type ExportCounts struct {
	Measurements int64
	States       int64
	Sessions     int64
	SessionStats int64
	Routes       int64
	Unmapped     int64
	Imports      int64
}

// ExportCounts counts every family for one Account. Called inside the export's
// read transaction, so the figures and the rows that follow are one snapshot.
func (m Models) ExportCounts(ctx context.Context, accountID int64) (ExportCounts, error) {
	var c ExportCounts
	counts := []struct {
		query string
		into  *int64
	}{
		{`SELECT COUNT(*) FROM measurements WHERE account_id = ?`, &c.Measurements},
		{`SELECT COUNT(*) FROM states WHERE account_id = ?`, &c.States},
		{`SELECT COUNT(*) FROM sessions WHERE account_id = ?`, &c.Sessions},
		{`SELECT COUNT(*) FROM session_stats
		    JOIN sessions ON sessions.id = session_stats.session_id
		   WHERE sessions.account_id = ?`, &c.SessionStats},
		{`SELECT COUNT(*) FROM routes WHERE account_id = ?`, &c.Routes},
		{`SELECT COUNT(*) FROM unmapped_records WHERE account_id = ?`, &c.Unmapped},
		{`SELECT COUNT(*) FROM imports WHERE account_id = ?`, &c.Imports},
	}
	for _, q := range counts {
		if err := m.Measurements.DB.QueryRowContext(ctx, q.query, accountID).Scan(q.into); err != nil {
			return ExportCounts{}, fmt.Errorf("data: export counts: %w", err)
		}
	}
	return c, nil
}
