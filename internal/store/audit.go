package store

import (
	"context"
	"encoding/json"
	"time"
)

// NewAuditEvent describes an audit event to append. Domain code records events through
// audit.Record, whose constructors keep secrets and health values out of the payload.
type NewAuditEvent struct {
	// ActorID is the user who acted, or 0 for the system.
	ActorID int64
	// SubjectID is the user the event is about, or 0 for none.
	SubjectID int64
	Type      string
	// ClientIP and UserAgent describe the client of the request, or are empty outside requests.
	ClientIP  string
	UserAgent string
	Payload   json.RawMessage
}

// AuditEvent is an entry of the audit log. Times are in UTC with millisecond precision. Events have
// no link to the users table: they outlive their users and keep their ids.
type AuditEvent struct {
	ID         int64
	OccurredAt time.Time
	// ActorID is the user who acted, or 0 for the system.
	ActorID int64
	// SubjectID is the user the event is about, or 0 for none.
	SubjectID int64
	Type      string
	// ClientIP and UserAgent describe the client of the request, or are empty outside requests.
	ClientIP  string
	UserAgent string
	Payload   json.RawMessage
	// Count is the number of occurrences this row stands for, and LastAt the time of the last one.
	Count  int64
	LastAt time.Time
}

// AuditEventsRepository stores the audit log. It is append-only: no method edits or deletes an
// event by id.
type AuditEventsRepository interface {
	// Append stores an event with the current time as OccurredAt, and returns it.
	Append(ctx context.Context, event NewAuditEvent) (AuditEvent, error)
	// Get returns the event of id, or ErrNotFound.
	Get(ctx context.Context, id int64) (AuditEvent, error)
}
