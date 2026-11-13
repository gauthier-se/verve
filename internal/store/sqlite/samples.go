package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// samples implements store.SamplesRepository.
type samples struct {
	q   queries
	clk clock.Clock
}

// Upsert checks the whole batch, its types and sources included, before its first write, so that
// a refused batch writes nothing even in a WithTx whose caller commits anyway. Updates never move
// the start of a sample, so only inserts change the data availability, written once per type.
func (r samples) Upsert(ctx context.Context, userID int64, batch []store.NewSample) (store.UpsertResult, error) {
	for i, sample := range batch {
		if err := checkSample(sample); err != nil {
			return store.UpsertResult{}, fmt.Errorf("upsert samples of user %d: sample %d: %w", userID, i, err)
		}
	}

	var result store.UpsertResult

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		if err := checkReferences(ctx, q, userID, batch); err != nil {
			return err
		}

		inserted := make(map[int64]sqlcgen.AddSampleStatsParams)

		for _, sample := range batch {
			outcome, err := upsertSample(ctx, q, userID, sample)
			if err != nil {
				return err
			}

			switch outcome {
			case sampleInserted:
				result.Inserted++

				inserted[sample.TypeID] = widenStats(inserted[sample.TypeID], userID, sample)
			case sampleUpdated:
				result.Updated++
			case sampleDuplicate:
				result.Duplicates++
			}
		}

		for _, typeID := range slices.Sorted(maps.Keys(inserted)) {
			if err := q.AddSampleStats(ctx, inserted[typeID]); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return store.UpsertResult{}, fmt.Errorf("upsert samples of user %d: %w", userID, err)
	}

	return result, nil
}

func (r samples) Stats(ctx context.Context, userID int64) ([]store.SampleStats, error) {
	rows, err := r.q.read().ListUserSampleStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sample stats of user %d: %w", userID, err)
	}

	stats := make([]store.SampleStats, 0, len(rows))
	for _, row := range rows {
		stats = append(stats, store.SampleStats{
			TypeID:  row.TypeID,
			FirstAt: fromMillis(row.FirstAt),
			LastAt:  fromMillis(row.LastAt),
			Count:   row.Count,
		})
	}

	return stats, nil
}

func (r samples) List(ctx context.Context, userID int64, query store.SamplesQuery) ([]store.Sample, error) {
	if query.Limit < 1 || !query.To.After(query.From) {
		return nil, fmt.Errorf("list samples of user %d: limit %d, from %s to %s: %w", userID, query.Limit,
			query.From, query.To, store.ErrInvalid)
	}

	rows, err := r.q.read().ListUserSamples(ctx, sqlcgen.ListUserSamplesParams{
		UserID:     userID,
		TypeID:     query.TypeID,
		FromAt:     toMillis(query.From),
		ToAt:       toMillis(query.To),
		AfterAt:    toMillis(query.After.At),
		AfterID:    query.After.ID,
		MaxSamples: int64(query.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list samples of user %d: %w", userID, err)
	}

	list := make([]store.Sample, 0, len(rows))
	for _, row := range rows {
		list = append(list, sampleOf(row))
	}

	return list, nil
}

func (r samples) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete samples of user %d: limit %d below 1: %w", userID, limit, store.ErrInvalid)
	}

	var deleted int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		deleted, err = q.DeleteUserSamples(ctx, sqlcgen.DeleteUserSamplesParams{UserID: userID, BatchSize: int64(limit)})
		if err != nil || deleted == int64(limit) {
			return err
		}

		statsDeleted, err := q.DeleteUserSampleStats(ctx, sqlcgen.DeleteUserSampleStatsParams{
			UserID:    userID,
			BatchSize: int64(limit) - deleted,
		})
		deleted += statsDeleted

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete samples of user %d: %w", userID, err)
	}

	return deleted, nil
}

// DeleteByKey removes the data availability of the type of the sample with its last sample, and
// otherwise counts one sample less and narrows it to the samples left.
func (r samples) DeleteByKey(ctx context.Context, userID int64, key []byte) (store.Sample, error) {
	var removed store.Sample

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		row, err := q.DeleteUserSampleByNaturalKey(ctx, sqlcgen.DeleteUserSampleByNaturalKeyParams{UserID: userID, NaturalKey: key})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		removed = sampleOf(row)

		gone, err := q.DeleteLastSampleStats(ctx, sqlcgen.DeleteLastSampleStatsParams{UserID: userID, TypeID: row.TypeID})
		if err != nil || gone > 0 {
			return err
		}

		return q.RemoveSampleStats(ctx, sqlcgen.RemoveSampleStatsParams{UserID: userID, TypeID: row.TypeID})
	})
	if err != nil {
		return store.Sample{}, fmt.Errorf("delete sample of user %d by key: %w", userID, err)
	}

	return removed, nil
}

// sampleOutcome is what an upsert did with a sample.
type sampleOutcome int

const (
	sampleInserted sampleOutcome = iota
	sampleUpdated
	sampleDuplicate
)

// checkSample returns ErrInvalid when sample has a field that the store refuses, the references to
// its type and source aside.
func checkSample(sample store.NewSample) error {
	switch {
	case !sample.OnConflict.Known():
		return fmt.Errorf("conflict rule %q: %w", sample.OnConflict, store.ErrInvalid)
	case !sample.Origin.Known():
		return fmt.Errorf("origin %q: %w", sample.Origin, store.ErrInvalid)
	case len(sample.NaturalKey) == 0:
		return fmt.Errorf("empty natural key: %w", store.ErrInvalid)
	case toMillis(sample.End) < toMillis(sample.Start):
		return fmt.Errorf("end before start: %w", store.ErrInvalid)
	case sample.Metadata != nil && !isJSONObject(sample.Metadata):
		return fmt.Errorf("metadata is not a JSON object: %w", store.ErrInvalid)
	default:
		return nil
	}
}

// isJSONObject reports whether data is a valid JSON object.
func isJSONObject(data []byte) bool {
	return json.Valid(data) && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{"))
}

// checkReferences returns ErrInvalid when a sample of batch has a type that does not exist or a
// source that the user of userID does not have. Each type and source is read once.
func checkReferences(ctx context.Context, q *sqlcgen.Queries, userID int64, batch []store.NewSample) error {
	types := make(map[int64]bool)
	sources := make(map[int64]bool)

	for _, sample := range batch {
		types[sample.TypeID] = true
		sources[sample.SourceID] = true
	}

	for typeID := range types {
		_, err := q.GetMetricType(ctx, typeID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("type %d: %w", typeID, store.ErrInvalid)
		}

		if err != nil {
			return err
		}
	}

	for sourceID := range sources {
		_, err := q.GetUserSource(ctx, sqlcgen.GetUserSourceParams{UserID: userID, ID: sourceID})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("source %d: %w", sourceID, store.ErrInvalid)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// upsertSample inserts sample for the user of userID, or applies its rule to the stored sample of
// its natural key.
func upsertSample(ctx context.Context, q *sqlcgen.Queries, userID int64, sample store.NewSample) (sampleOutcome, error) {
	_, err := q.InsertSample(ctx, sqlcgen.InsertSampleParams{
		UserID:     userID,
		TypeID:     sample.TypeID,
		SourceID:   sample.SourceID,
		StartAt:    toMillis(sample.Start),
		EndAt:      toMillis(sample.End),
		UtcOffset:  int64(sample.UTCOffset),
		Value:      sample.Value,
		Min:        nullFloat(sample.Min),
		Max:        nullFloat(sample.Max),
		Category:   sample.Category,
		Origin:     string(sample.Origin),
		ImportID:   sample.ImportID,
		Uuid:       sample.UUID,
		NaturalKey: sample.NaturalKey,
		Metadata:   nullJSON(sample.Metadata),
	})
	if err == nil {
		return sampleInserted, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return 0, constraintError(err)
	}

	updated, err := applyConflictRule(ctx, q, userID, sample)
	if err != nil {
		return 0, err
	}

	if updated > 0 {
		return sampleUpdated, nil
	}

	return sampleDuplicate, nil
}

// applyConflictRule applies the rule of sample to the stored sample of its natural key, of the user
// of userID, and returns the number of samples it changed: 1 or 0.
func applyConflictRule(ctx context.Context, q *sqlcgen.Queries, userID int64, sample store.NewSample) (int64, error) {
	switch sample.OnConflict {
	case store.ConflictUpdateIfChanged:
		return q.UpdateSampleIfChanged(ctx, sqlcgen.UpdateSampleIfChangedParams{
			Value:      sample.Value,
			Min:        nullFloat(sample.Min),
			Max:        nullFloat(sample.Max),
			Category:   sample.Category,
			UserID:     userID,
			NaturalKey: sample.NaturalKey,
		})
	case store.ConflictReplace:
		return q.ReplaceSample(ctx, sqlcgen.ReplaceSampleParams{
			Value:      sample.Value,
			Min:        nullFloat(sample.Min),
			Max:        nullFloat(sample.Max),
			UserID:     userID,
			NaturalKey: sample.NaturalKey,
		})
	case store.ConflictIgnore:
	}

	return 0, nil
}

// widenStats returns stats, the data availability of the samples of a type inserted by a batch so
// far, with sample.
func widenStats(stats sqlcgen.AddSampleStatsParams, userID int64, sample store.NewSample) sqlcgen.AddSampleStatsParams {
	start := toMillis(sample.Start)

	if stats.Count == 0 {
		return sqlcgen.AddSampleStatsParams{UserID: userID, TypeID: sample.TypeID, FirstAt: start, LastAt: start, Count: 1}
	}

	stats.FirstAt = min(stats.FirstAt, start)
	stats.LastAt = max(stats.LastAt, start)
	stats.Count++

	return stats
}

func nullFloat(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}

	return sql.NullFloat64{Float64: *v, Valid: true}
}

func fromNullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}

	return &v.Float64
}

func nullJSON(data json.RawMessage) sql.NullString {
	if data == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: string(data), Valid: true}
}

func sampleOf(row sqlcgen.Sample) store.Sample {
	var metadata json.RawMessage
	if row.Metadata.Valid {
		metadata = json.RawMessage(row.Metadata.String)
	}

	return store.Sample{
		ID:         row.ID,
		UserID:     row.UserID,
		TypeID:     row.TypeID,
		SourceID:   row.SourceID,
		Start:      fromMillis(row.StartAt),
		End:        fromMillis(row.EndAt),
		UTCOffset:  int(row.UtcOffset),
		Value:      row.Value,
		Min:        fromNullFloat(row.Min),
		Max:        fromNullFloat(row.Max),
		Category:   row.Category,
		Origin:     store.Origin(row.Origin),
		ImportID:   row.ImportID,
		UUID:       row.Uuid,
		NaturalKey: row.NaturalKey,
		Metadata:   metadata,
	}
}
