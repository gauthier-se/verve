package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// Once burstThreshold aggregatable events of a type and key happened within burstWindow, the next
// ones go into the count of their latest row.
const (
	burstThreshold = 20
	burstWindow    = 10 * time.Minute
)

// auditEvents implements store.AuditEventsRepository.
type auditEvents struct {
	q   queries
	clk clock.Clock
}

func (r auditEvents) Append(ctx context.Context, event store.NewAuditEvent) (store.AuditEvent, error) {
	var row sqlcgen.AuditEvent

	// One write transaction, so that no other event of the burst comes between the count and the
	// write.
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		now := r.clk.Now()
		aggregationKey := sql.NullString{String: event.AggregationKey, Valid: event.AggregationKey != ""}

		if aggregationKey.Valid {
			burst, err := q.GetAuditBurst(ctx, sqlcgen.GetAuditBurstParams{
				Type:           event.Type,
				AggregationKey: aggregationKey,
				Since:          toMillis(now.Add(-burstWindow)),
			})
			if err != nil {
				return err
			}

			if burst.Events >= burstThreshold {
				row, err = q.AggregateAuditEvent(ctx, sqlcgen.AggregateAuditEventParams{LastAt: toMillis(now), ID: burst.LatestID})

				return err
			}
		}

		var err error

		row, err = q.AppendAuditEvent(ctx, sqlcgen.AppendAuditEventParams{
			OccurredAt:     toMillis(now),
			ActorID:        sql.NullInt64{Int64: event.ActorID, Valid: event.ActorID != 0},
			SubjectID:      sql.NullInt64{Int64: event.SubjectID, Valid: event.SubjectID != 0},
			Type:           event.Type,
			ClientIp:       sql.NullString{String: event.ClientIP, Valid: event.ClientIP != ""},
			UserAgent:      sql.NullString{String: event.UserAgent, Valid: event.UserAgent != ""},
			Payload:        string(event.Payload),
			AggregationKey: aggregationKey,
		})

		return err
	})
	if err != nil {
		return store.AuditEvent{}, fmt.Errorf("append audit event of type %q: %w", event.Type, err)
	}

	return auditEventOf(row), nil
}

func (r auditEvents) Get(ctx context.Context, id int64) (store.AuditEvent, error) {
	row, err := r.q.read().GetAuditEvent(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.AuditEvent{}, fmt.Errorf("get audit event %d: %w", id, err)
	}

	return auditEventOf(row), nil
}

func (r auditEvents) List(ctx context.Context, filter store.AuditEventsFilter, page store.Page) ([]store.AuditEvent, error) {
	if page.Limit < 1 {
		return nil, fmt.Errorf("list audit events: limit %d below 1: %w", page.Limit, store.ErrInvalid)
	}

	// The bounds that select every event, so that the query always has them.
	params := sqlcgen.ListAuditEventsParams{
		AfterAt:   math.MaxInt64,
		AfterID:   math.MaxInt64,
		ToAt:      math.MaxInt64,
		FromAt:    math.MinInt64,
		UserID:    sql.NullInt64{Int64: filter.UserID, Valid: filter.UserID != 0},
		ActorID:   sql.NullInt64{Int64: filter.ActorID, Valid: filter.ActorID != 0},
		SubjectID: sql.NullInt64{Int64: filter.SubjectID, Valid: filter.SubjectID != 0},
		Type:      sql.NullString{String: filter.Type, Valid: filter.Type != ""},
		MaxEvents: int64(page.Limit),
	}

	if page.After != (store.Cursor{}) {
		params.AfterAt = toMillis(page.After.At)
		params.AfterID = page.After.ID
	}

	if !filter.From.IsZero() {
		params.FromAt = toMillis(filter.From)
	}

	if !filter.To.IsZero() {
		params.ToAt = toMillis(filter.To)
	}

	rows, err := r.q.read().ListAuditEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}

	events := make([]store.AuditEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, auditEventOf(row))
	}

	return events, nil
}

func (r auditEvents) PurgeBefore(ctx context.Context, cutoff time.Time, batchSize int64) (int64, error) {
	var purged int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		purged, err = q.PurgeAuditEvents(ctx, sqlcgen.PurgeAuditEventsParams{Cutoff: toMillis(cutoff), BatchSize: batchSize})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("purge audit events: %w", err)
	}

	return purged, nil
}

func auditEventOf(row sqlcgen.AuditEvent) store.AuditEvent {
	return store.AuditEvent{
		ID:         row.ID,
		OccurredAt: fromMillis(row.OccurredAt),
		ActorID:    row.ActorID.Int64,
		SubjectID:  row.SubjectID.Int64,
		Type:       row.Type,
		ClientIP:   row.ClientIp.String,
		UserAgent:  row.UserAgent.String,
		Payload:    json.RawMessage(row.Payload),
		Count:      row.Count,
		LastAt:     fromMillis(row.LastAt),
	}
}
