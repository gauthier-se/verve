package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Session is a rich, bounded activity (a workout), owned by one Account: an
// ActivityType over an interval, with Duration and optional totals. Duration is
// seconds, TotalDistance km, TotalEnergy kcal (canonical units). TotalDistance
// and TotalEnergy are nil when the source reports none (a strength session has
// no distance). ContentKey is the workout's stable identity (ADR 0006).
type Session struct {
	ID            int64
	AccountID     int64
	ActivityType  string
	StartAt       string
	EndAt         string
	Duration      float64
	TotalDistance *float64
	TotalEnergy   *float64
	Source        string
	ContentKey    string
}

// Route is a GPS track (GPX) attached to a Session. The .gpx lives on disk under
// VERVE_DATA_DIR/artifacts/ (ADR 0004); Artifact is its filename there.
// ContentKey is the sha256 of the file contents, so the artifact is
// content-addressed and re-import is idempotent (ADR 0006).
type Route struct {
	ID         int64
	AccountID  int64
	SessionID  int64
	Artifact   string
	StartAt    string
	EndAt      string
	Source     string
	ContentKey string
}

// SessionStat is one summary figure a Session carries: an aggregate of a
// canonical Metric over the whole workout: average heart rate, maximum heart
// rate, total steps. Stat is the aggregate Apple reported (StatSum, StatAverage,
// StatMin, StatMax) and Value is in the Metric's canonical unit, so a stat and a
// Measurement of the same quantity are directly comparable.
//
// TotalDistance and TotalEnergy on Session are the two promoted stats: they are
// also rows here, and that duplication is deliberate, because the workout list sorts
// and displays from the columns without a join per row.
type SessionStat struct {
	Metric string
	Stat   string
	Value  float64
}

// The aggregates Apple reports per <WorkoutStatistics>. An average and a maximum
// are different answers to different questions, so all four are kept.
const (
	StatSum     = "sum"
	StatAverage = "average"
	StatMin     = "min"
	StatMax     = "max"
)

// SessionModel is the DAO for sessions (workouts), their stats and their routes.
type SessionModel struct {
	DB *sql.DB
}

// InsertSession inserts one Session, deduped per account by content key. It sets
// s.ID to the row's id — the new id when inserted, or the existing row's id when
// a matching workout was already imported — and returns whether it was newly
// inserted. Sessions are few (hundreds per export), so unlike measurements they
// are written one at a time: the caller needs each id to attach the workout's
// routes as it parses (ADR 0006 idempotency still holds via content_key).
func (m SessionModel) InsertSession(ctx context.Context, s *Session) (bool, error) {
	const query = `
		INSERT OR IGNORE INTO sessions
			(account_id, activity_type, start_at, end_at, duration, total_distance, total_energy, source, content_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := m.DB.ExecContext(ctx, query,
		s.AccountID, s.ActivityType, s.StartAt, s.EndAt, s.Duration,
		s.TotalDistance, s.TotalEnergy, s.Source, s.ContentKey)
	if err != nil {
		return false, fmt.Errorf("data: insert session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("data: session rows affected: %w", err)
	}
	if n == 1 {
		id, err := res.LastInsertId()
		if err != nil {
			return false, fmt.Errorf("data: session last insert id: %w", err)
		}
		s.ID = id
		return true, nil
	}

	// Already imported: recover its id so routes can still be attached.
	if err := m.DB.QueryRowContext(ctx,
		`SELECT id FROM sessions WHERE account_id = ? AND content_key = ?`,
		s.AccountID, s.ContentKey).Scan(&s.ID); err != nil {
		return false, fmt.Errorf("data: lookup existing session: %w", err)
	}
	return false, nil
}

// InsertRoute inserts one Route, deduped per account by content key (the file's
// content hash), and returns whether it was newly inserted. Idempotent on
// re-import: the same GPX yields the same content key and is skipped.
func (m SessionModel) InsertRoute(ctx context.Context, r *Route) (bool, error) {
	const query = `
		INSERT OR IGNORE INTO routes
			(account_id, session_id, artifact, start_at, end_at, source, content_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := m.DB.ExecContext(ctx, query,
		r.AccountID, r.SessionID, r.Artifact, r.StartAt, r.EndAt, r.Source, r.ContentKey)
	if err != nil {
		return false, fmt.Errorf("data: insert route: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("data: route rows affected: %w", err)
	}
	return n == 1, nil
}

// InsertSessionStats records a Session's summary figures, keyed by
// (session, metric, stat). Conflicts update the value rather than being ignored:
// a re-import of a corrected export must correct the stored figure, and a stat
// that can never be fixed is worse than one that is occasionally rewritten.
//
// This runs on every import of a workout, not only the first, which is what
// makes a re-import converge: a Session already in the database still gains the
// stats a widened import now captures.
func (m SessionModel) InsertSessionStats(ctx context.Context, sessionID int64, stats []SessionStat) error {
	if len(stats) == 0 {
		return nil
	}
	const query = `
		INSERT INTO session_stats (session_id, metric, stat, value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (session_id, metric, stat) DO UPDATE SET value = excluded.value`
	stmt, err := m.DB.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("data: prepare session stats: %w", err)
	}
	defer stmt.Close()
	for _, s := range stats {
		if _, err := stmt.ExecContext(ctx, sessionID, s.Metric, s.Stat, s.Value); err != nil {
			return fmt.Errorf("data: insert session stat %s/%s: %w", s.Metric, s.Stat, err)
		}
	}
	return nil
}

// SessionFilter narrows a workout listing. From and To are RFC 3339 UTC bounds
// on start_at, inclusive and exclusive respectively. Activities and
// ActivityGroups are OR-ed within themselves and AND-ed with each other; an
// empty filter means every workout the Account owns.
//
// Cursor is the (start_at, id) pair of the last row of the previous page, empty
// on the first. It is a keyset and not an offset: an import running while
// someone browses inserts older workouts underneath the reader, and an offset
// would silently skip or repeat rows at every page boundary.
type SessionFilter struct {
	From          string
	To            string
	Activities    []string
	ExcludeActs   []string
	CursorStartAt string
	CursorID      int64
	Limit         int
}

// SessionTotals describes a whole filter, not a page: the count of Sessions it
// matches and the sums over them. Distance and Energy are nil when no matching
// Session reported one, which is a gap and not a zero.
type SessionTotals struct {
	Count    int
	Duration float64
	Distance *float64
	Energy   *float64
}

// where builds the shared predicate of a filter, minus the cursor.
func (f SessionFilter) where(accountID int64) (string, []any) {
	clause := " WHERE account_id = ?"
	args := []any{accountID}
	if f.From != "" {
		clause += " AND start_at >= ?"
		args = append(args, f.From)
	}
	if f.To != "" {
		clause += " AND start_at < ?"
		args = append(args, f.To)
	}
	if len(f.Activities) > 0 {
		clause += " AND activity_type IN (" + placeholders(len(f.Activities)) + ")"
		for _, a := range f.Activities {
			args = append(args, a)
		}
	}
	if len(f.ExcludeActs) > 0 {
		clause += " AND activity_type NOT IN (" + placeholders(len(f.ExcludeActs)) + ")"
		for _, a := range f.ExcludeActs {
			args = append(args, a)
		}
	}
	return clause, args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	s := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			s = append(s, ',')
		}
		s = append(s, '?')
	}
	return string(s)
}

// ListSessions returns one page of an Account's workouts, newest first, together
// with whether each carries a Route. Ordering is (start_at, id) descending, which
// is what the cursor addresses; sessions_account_start covers it.
func (m SessionModel) ListSessions(ctx context.Context, accountID int64, f SessionFilter) ([]Session, []bool, error) {
	clause, args := f.where(accountID)
	if f.CursorStartAt != "" {
		clause += " AND (start_at < ? OR (start_at = ? AND id < ?))"
		args = append(args, f.CursorStartAt, f.CursorStartAt, f.CursorID)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, activity_type, start_at, end_at, duration, total_distance, total_energy, source,
		       EXISTS (SELECT 1 FROM routes r WHERE r.session_id = sessions.id)
		  FROM sessions` + clause + `
		 ORDER BY start_at DESC, id DESC
		 LIMIT ?`
	rows, err := m.DB.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, nil, fmt.Errorf("data: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	var hasRoute []bool
	for rows.Next() {
		s := Session{AccountID: accountID}
		var routed bool
		if err := rows.Scan(&s.ID, &s.ActivityType, &s.StartAt, &s.EndAt, &s.Duration,
			&s.TotalDistance, &s.TotalEnergy, &s.Source, &routed); err != nil {
			return nil, nil, fmt.Errorf("data: scan session: %w", err)
		}
		sessions = append(sessions, s)
		hasRoute = append(hasRoute, routed)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("data: list sessions rows: %w", err)
	}
	return sessions, hasRoute, nil
}

// SessionTotals folds a filter, ignoring its cursor. The header a listing shows
// describes what was asked for, not what fits on the current page: folding the
// returned rows instead is one line shorter and produces a figure that looks
// like a total and is a page sum.
func (m SessionModel) SessionTotals(ctx context.Context, accountID int64, f SessionFilter) (SessionTotals, error) {
	clause, args := f.where(accountID)
	query := `
		SELECT count(*), coalesce(sum(duration), 0), sum(total_distance), sum(total_energy)
		  FROM sessions` + clause
	var t SessionTotals
	if err := m.DB.QueryRowContext(ctx, query, args...).
		Scan(&t.Count, &t.Duration, &t.Distance, &t.Energy); err != nil {
		return SessionTotals{}, fmt.Errorf("data: session totals: %w", err)
	}
	return t, nil
}

// GetSession returns one Session owned by accountID. Ownership is in the WHERE
// clause rather than checked after the fact, so a missing row and another
// Account's row are the same outcome (ADR 0007).
func (m SessionModel) GetSession(ctx context.Context, accountID, id int64) (Session, error) {
	const query = `
		SELECT id, account_id, activity_type, start_at, end_at, duration,
		       total_distance, total_energy, source, content_key
		  FROM sessions WHERE id = ? AND account_id = ?`
	var s Session
	err := m.DB.QueryRowContext(ctx, query, id, accountID).Scan(
		&s.ID, &s.AccountID, &s.ActivityType, &s.StartAt, &s.EndAt, &s.Duration,
		&s.TotalDistance, &s.TotalEnergy, &s.Source, &s.ContentKey)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrRecordNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("data: get session: %w", err)
	}
	return s, nil
}

// SessionStats returns one Session's summary figures, ordered by metric then by
// aggregate so a detail payload is stable between requests.
func (m SessionModel) SessionStats(ctx context.Context, accountID, sessionID int64) ([]SessionStat, error) {
	const query = `
		SELECT s.metric, s.stat, s.value
		  FROM session_stats s JOIN sessions w ON w.id = s.session_id
		 WHERE s.session_id = ? AND w.account_id = ?
		 ORDER BY s.metric, s.stat`
	rows, err := m.DB.QueryContext(ctx, query, sessionID, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: session stats: %w", err)
	}
	defer rows.Close()

	stats := []SessionStat{}
	for rows.Next() {
		var s SessionStat
		if err := rows.Scan(&s.Metric, &s.Stat, &s.Value); err != nil {
			return nil, fmt.Errorf("data: scan session stat: %w", err)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// RoutesForSession returns a Session's Routes in recorded order. A Session may
// carry several and they stay several: they are segments of the same workout,
// and joining them would draw a line across ground nobody covered (ADR 0028).
func (m SessionModel) RoutesForSession(ctx context.Context, accountID, sessionID int64) ([]Route, error) {
	const query = `
		SELECT r.id, r.account_id, r.session_id, r.artifact, r.start_at, r.end_at, r.source, r.content_key
		  FROM routes r JOIN sessions w ON w.id = r.session_id
		 WHERE r.session_id = ? AND w.account_id = ?
		 ORDER BY r.start_at, r.id`
	rows, err := m.DB.QueryContext(ctx, query, sessionID, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: routes for session: %w", err)
	}
	defer rows.Close()

	routes := []Route{}
	for rows.Next() {
		var r Route
		if err := rows.Scan(&r.ID, &r.AccountID, &r.SessionID, &r.Artifact,
			&r.StartAt, &r.EndAt, &r.Source, &r.ContentKey); err != nil {
			return nil, fmt.Errorf("data: scan route: %w", err)
		}
		routes = append(routes, r)
	}
	return routes, rows.Err()
}
