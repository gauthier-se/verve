package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// metricTypes implements store.MetricTypesRepository.
type metricTypes struct {
	q   queries
	clk clock.Clock
}

// Sync first gives every row whose key changes a temporary key, so that no statement of the
// transaction breaks the uniqueness of the keys, such as when two types swap theirs.
func (r metricTypes) Sync(ctx context.Context, c *catalog.Catalog) error {
	now := toMillis(r.clk.Now())
	types := c.Types()

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		rows, err := q.ListMetricTypes(ctx)
		if err != nil {
			return err
		}

		keyOf := make(map[string]string, len(types)) // catalog key by identifier
		taken := make(map[string]bool, len(rows)+len(types))

		for _, t := range types {
			keyOf[t.Identifier] = t.Key
			taken[t.Key] = true
		}

		// unlisted are the rows that the catalog does not list and that keep their key, renamed those
		// whose key the catalog gives to another type, and moved every row whose key changes, renamed
		// ones included.
		var unlisted, renamed, moved []sqlcgen.MetricType

		for _, row := range rows {
			key, listed := keyOf[row.Identifier]

			switch {
			case listed:
				if row.Key != key {
					moved = append(moved, row)
				}
			case taken[row.Key]:
				renamed = append(renamed, row)
				moved = append(moved, row)
			default:
				unlisted = append(unlisted, row)
				taken[row.Key] = true
			}
		}

		for _, row := range moved {
			if err := q.SetMetricTypeKey(ctx, sqlcgen.SetMetricTypeKeyParams{Key: temporaryKey(row.ID), ID: row.ID}); err != nil {
				return fmt.Errorf("type %s: %w", row.Identifier, err)
			}
		}

		for _, row := range renamed {
			key := freeKey(catalog.UncuratedKey(row.Identifier), taken)
			taken[key] = true

			if err := q.SetMetricTypeKey(ctx, sqlcgen.SetMetricTypeKeyParams{Key: key, ID: row.ID}); err != nil {
				return fmt.Errorf("type %s: %w", row.Identifier, err)
			}
		}

		for _, row := range slices.Concat(unlisted, renamed) {
			if row.Curated == 0 {
				continue
			}

			if err := q.UncurateMetricType(ctx, row.ID); err != nil {
				return fmt.Errorf("type %s: %w", row.Identifier, err)
			}
		}

		for _, t := range types {
			err := q.SyncMetricType(ctx, sqlcgen.SyncMetricTypeParams{
				Key:          t.Key,
				Identifier:   t.Identifier,
				Kind:         string(t.Kind),
				Unit:         t.Unit,
				Aggregation:  string(t.Aggregation),
				DisplayGroup: string(t.Group),
				CreatedAt:    now,
			})
			if err != nil {
				return fmt.Errorf("type %s: %w", t.Identifier, err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("sync metric types: %w", err)
	}

	return nil
}

// EnsureUncurated reads the type on the reader pool first, so that a type already stored, the
// common case, does not wait for the writer.
func (r metricTypes) EnsureUncurated(ctx context.Context, identifier string, kind catalog.Kind, unit string,
) (store.MetricType, error) {
	if identifier == "" || !kind.Known() {
		return store.MetricType{}, fmt.Errorf("ensure metric type %q of kind %q: %w", identifier, kind, store.ErrInvalid)
	}

	row, err := r.q.read().GetMetricTypeByIdentifier(ctx, identifier)
	if err == nil {
		return metricTypeOf(row), nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return store.MetricType{}, fmt.Errorf("ensure metric type %q: %w", identifier, err)
	}

	err = r.q.write(ctx, func(q *sqlcgen.Queries) error {
		keys, err := q.ListMetricTypeKeys(ctx)
		if err != nil {
			return err
		}

		taken := make(map[string]bool, len(keys))
		for _, key := range keys {
			taken[key] = true
		}

		// Another call may have created the type since the read: the insert then does nothing, and
		// the read returns that type.
		err = q.CreateUncuratedMetricType(ctx, sqlcgen.CreateUncuratedMetricTypeParams{
			Key:        freeKey(catalog.UncuratedKey(identifier), taken),
			Identifier: identifier,
			Kind:       string(kind),
			Unit:       unit,
			CreatedAt:  toMillis(r.clk.Now()),
		})
		if err != nil {
			return err
		}

		row, err = q.GetMetricTypeByIdentifier(ctx, identifier)

		return err
	})
	if err != nil {
		return store.MetricType{}, fmt.Errorf("ensure metric type %q: %w", identifier, err)
	}

	return metricTypeOf(row), nil
}

func (r metricTypes) Get(ctx context.Context, id int64) (store.MetricType, error) {
	row, err := r.q.read().GetMetricType(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.MetricType{}, fmt.Errorf("get metric type %d: %w", id, err)
	}

	return metricTypeOf(row), nil
}

func (r metricTypes) GetByKey(ctx context.Context, key string) (store.MetricType, error) {
	row, err := r.q.read().GetMetricTypeByKey(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.MetricType{}, fmt.Errorf("get metric type by key %q: %w", key, err)
	}

	return metricTypeOf(row), nil
}

func (r metricTypes) GetByIdentifier(ctx context.Context, identifier string) (store.MetricType, error) {
	row, err := r.q.read().GetMetricTypeByIdentifier(ctx, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.MetricType{}, fmt.Errorf("get metric type by identifier %q: %w", identifier, err)
	}

	return metricTypeOf(row), nil
}

func (r metricTypes) List(ctx context.Context) ([]store.MetricType, error) {
	rows, err := r.q.read().ListMetricTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list metric types: %w", err)
	}

	types := make([]store.MetricType, 0, len(rows))
	for _, row := range rows {
		types = append(types, metricTypeOf(row))
	}

	return types, nil
}

func metricTypeOf(row sqlcgen.MetricType) store.MetricType {
	return store.MetricType{
		ID:          row.ID,
		Key:         row.Key,
		Identifier:  row.Identifier,
		Kind:        catalog.Kind(row.Kind),
		Unit:        row.Unit,
		Aggregation: catalog.Aggregation(row.Aggregation),
		Group:       catalog.Group(row.DisplayGroup),
		Curated:     row.Curated == 1,
		CreatedAt:   fromMillis(row.CreatedAt),
	}
}

// temporaryKey is the key of the row of id while Sync moves keys. No type has a key with a colon.
func temporaryKey(id int64) string {
	return "sync:" + strconv.FormatInt(id, 10)
}

// freeKey returns base, or base followed by _2, _3 and so on, the first that is not taken.
func freeKey(base string, taken map[string]bool) string {
	key := base

	for n := 2; taken[key]; n++ {
		key = base + "_" + strconv.Itoa(n)
	}

	return key
}
