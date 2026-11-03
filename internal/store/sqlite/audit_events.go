package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// A burst of aggregatable events goes into the count of its latest row from the burstThreshold-th
// event within burstWindow on.
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
