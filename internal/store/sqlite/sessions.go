package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// sessions implements store.SessionsRepository. Its errors never hold a hash: the hash of a session
// identifier stands for the identifier in the database.
type sessions struct {
	q   queries
	clk clock.Clock
}

func (r sessions) Create(ctx context.Context, session store.NewSession) (store.Session, error) {
	var row sqlcgen.Session

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		now := toMillis(r.clk.Now())

		var err error

		row, err = q.CreateSession(ctx, sqlcgen.CreateSessionParams{
			IDHash:     session.IDHash,
			PublicID:   session.PublicID,
			UserID:     session.UserID,
			Method:     string(session.Method),
			CreatedAt:  now,
			LastSeenAt: now,
			ExpiresAt:  toMillis(session.ExpiresAt),
			ClientIp:   sql.NullString{String: session.ClientIP, Valid: session.ClientIP != ""},
			UserAgent:  sql.NullString{String: session.UserAgent, Valid: session.UserAgent != ""},
		})

		// The only foreign key of a session is its user.
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
			return store.ErrNotFound
		}

		return constraintError(err)
	})
	if err != nil {
		return store.Session{}, fmt.Errorf("create session of user %d: %w", session.UserID, err)
	}

	return sessionOf(row), nil
}

func (r sessions) Get(ctx context.Context, idHash []byte) (store.Session, store.User, error) {
	row, err := r.q.read().GetSession(ctx, idHash)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Session{}, store.User{}, fmt.Errorf("get session: %w", err)
	}

	return sessionOf(row.Session), userOf(row.User), nil
}

func (r sessions) Touch(ctx context.Context, idHash []byte, expiresAt time.Time) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		touched, err := q.TouchSession(ctx, sqlcgen.TouchSessionParams{
			LastSeenAt: toMillis(r.clk.Now()),
			ExpiresAt:  toMillis(expiresAt),
			IDHash:     idHash,
		})
		if err != nil {
			return err
		}

		if touched == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}

	return nil
}

func (r sessions) Delete(ctx context.Context, idHash []byte) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		deleted, err := q.DeleteSession(ctx, idHash)
		if err != nil {
			return err
		}

		if deleted == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

func (r sessions) DeleteOthers(ctx context.Context, userID int64, keepIDHash []byte) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.DeleteOtherSessions(ctx, sqlcgen.DeleteOtherSessionsParams{UserID: userID, KeepIDHash: keepIDHash})
	})
	if err != nil {
		return fmt.Errorf("delete other sessions of user %d: %w", userID, err)
	}

	return nil
}

func (r sessions) PurgeExpired(ctx context.Context, now time.Time) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.PurgeExpiredSessions(ctx, toMillis(now))
	})
	if err != nil {
		return fmt.Errorf("purge expired sessions: %w", err)
	}

	return nil
}

func sessionOf(row sqlcgen.Session) store.Session {
	return store.Session{
		PublicID:   row.PublicID,
		UserID:     row.UserID,
		Method:     store.LoginMethod(row.Method),
		CreatedAt:  fromMillis(row.CreatedAt),
		LastSeenAt: fromMillis(row.LastSeenAt),
		ExpiresAt:  fromMillis(row.ExpiresAt),
		ClientIP:   row.ClientIp.String,
		UserAgent:  row.UserAgent.String,
	}
}
