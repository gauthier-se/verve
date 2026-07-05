package data

import (
	"context"
	"database/sql"
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

// SessionModel is the DAO for sessions (workouts) and their routes.
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
