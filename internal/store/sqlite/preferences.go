package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// preferences implements store.PreferencesRepository.
type preferences struct {
	q   queries
	clk clock.Clock
}

func (r preferences) Get(ctx context.Context, userID int64) (store.Preferences, error) {
	timezone, err := r.q.read().GetPreferences(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Preferences{}, fmt.Errorf("get preferences of user %d: %w", userID, err)
	}

	return store.Preferences{Timezone: timezone.String}, nil
}

func (r preferences) SetTimezone(ctx context.Context, userID int64, timezone string) (store.Preferences, error) {
	var row sqlcgen.UserPreference

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.SetTimezone(ctx, sqlcgen.SetTimezoneParams{
			UserID:   userID,
			Timezone: sql.NullString{String: timezone, Valid: true},
		})

		// The only foreign key of the preferences is their user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.Preferences{}, fmt.Errorf("set timezone of user %d: %w", userID, err)
	}

	return preferencesOf(row), nil
}

func preferencesOf(row sqlcgen.UserPreference) store.Preferences {
	return store.Preferences{Timezone: row.Timezone.String}
}
