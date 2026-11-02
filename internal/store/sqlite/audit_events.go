package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// auditEvents implements store.AuditEventsRepository.
type auditEvents struct {
	q   queries
	clk clock.Clock
}

func (r auditEvents) Append(ctx context.Context, event store.NewAuditEvent) (store.AuditEvent, error) {
	var row sqlcgen.AuditEvent

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.AppendAuditEvent(ctx, sqlcgen.AppendAuditEventParams{
			OccurredAt: toMillis(r.clk.Now()),
			ActorID:    sql.NullInt64{Int64: event.ActorID, Valid: event.ActorID != 0},
			SubjectID:  sql.NullInt64{Int64: event.SubjectID, Valid: event.SubjectID != 0},
			Type:       event.Type,
			ClientIp:   sql.NullString{String: event.ClientIP, Valid: event.ClientIP != ""},
			UserAgent:  sql.NullString{String: event.UserAgent, Valid: event.UserAgent != ""},
			Payload:    string(event.Payload),
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
