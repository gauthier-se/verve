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

// instanceSettings implements store.InstanceSettingsRepository.
type instanceSettings struct {
	q   queries
	clk clock.Clock
}

func (r instanceSettings) Get(ctx context.Context, key string) (store.Setting, error) {
	row, err := r.q.read().GetInstanceSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Setting{}, fmt.Errorf("get instance setting %q: %w", key, err)
	}

	return settingOf(row), nil
}

func (r instanceSettings) List(ctx context.Context) ([]store.Setting, error) {
	rows, err := r.q.read().ListInstanceSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instance settings: %w", err)
	}

	settings := make([]store.Setting, 0, len(rows))
	for _, row := range rows {
		settings = append(settings, settingOf(row))
	}

	return settings, nil
}

func (r instanceSettings) Set(ctx context.Context, key, value string) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.SetInstanceSetting(ctx, sqlcgen.SetInstanceSettingParams{
			Key:       key,
			Value:     value,
			UpdatedAt: toMillis(r.clk.Now()),
		})
	})
	if err != nil {
		return fmt.Errorf("set instance setting %q: %w", key, err)
	}

	return nil
}

func (r instanceSettings) Delete(ctx context.Context, key string) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		deleted, err := q.DeleteInstanceSetting(ctx, key)
		if err != nil {
			return err
		}

		if deleted == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("delete instance setting %q: %w", key, err)
	}

	return nil
}

func settingOf(row sqlcgen.InstanceSetting) store.Setting {
	return store.Setting{Key: row.Key, Value: row.Value, UpdatedAt: fromMillis(row.UpdatedAt)}
}
