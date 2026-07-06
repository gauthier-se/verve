package data

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// sqlTimeFormat matches the millisecond-precision UTC format the schema's
// timestamp DEFAULT uses (strftime('%Y-%m-%dT%H:%M:%fZ')), so a Go-computed
// expires_at and SQLite's `now` sort identically as strings.
const sqlTimeFormat = "2006-01-02T15:04:05.000Z07:00"

// AuthSession is a server-side login session backing an opaque cookie. It is the
// authentication concept, deliberately distinct from the workout Session family
// (see CONTEXT.md). Only TokenHash — the SHA-256 of the cookie's token — is
// stored, never the token itself.
type AuthSession struct {
	TokenHash string
	AccountID int64
	ExpiresAt time.Time
}

// AuthSessionModel is the DAO for login sessions.
type AuthSessionModel struct {
	DB *sql.DB
}

// Insert stores a new login session.
func (m AuthSessionModel) Insert(ctx context.Context, s AuthSession) error {
	const query = `
		INSERT INTO auth_sessions (token_hash, account_id, expires_at)
		VALUES (?, ?, ?)`
	_, err := m.DB.ExecContext(ctx, query, s.TokenHash, s.AccountID, s.ExpiresAt.UTC().Format(sqlTimeFormat))
	return err
}

// AccountIDByToken returns the Account owning the unexpired session with the
// given token hash, or ErrRecordNotFound when no live session matches. Expiry is
// evaluated in SQL against the same clock and format the column default uses.
func (m AuthSessionModel) AccountIDByToken(ctx context.Context, tokenHash string) (int64, error) {
	const query = `
		SELECT account_id FROM auth_sessions
		WHERE token_hash = ? AND expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`
	var accountID int64
	err := m.DB.QueryRowContext(ctx, query, tokenHash).Scan(&accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrRecordNotFound
		}
		return 0, err
	}
	return accountID, nil
}

// Delete removes the session with the given token hash (logout). Deleting an
// absent or already-expired session is not an error — logout is idempotent.
func (m AuthSessionModel) Delete(ctx context.Context, tokenHash string) error {
	_, err := m.DB.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpired purges sessions past their expiry and returns how many it
// removed, for periodic housekeeping.
func (m AuthSessionModel) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := m.DB.ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
