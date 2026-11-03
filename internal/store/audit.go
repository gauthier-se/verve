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
	// AggregationKey makes the event aggregatable: bursts of the events of its type and key share a
	// row. It is empty for an event that is never aggregated.
	AggregationKey string
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
// event by id, and only PurgeBefore removes events, by age.
type AuditEventsRepository interface {
	// Append stores an event with the current time as OccurredAt, and returns it.
	//
	// An aggregatable event is aggregated during a burst: when 20 or more events of its type and
	// aggregation key happened in the last 10 minutes, Append increments the Count of their latest
	// row, sets its LastAt to the current time and returns that row, without adding one. The events
	// of a row count as recent while its LastAt is, so a burst whose latest row holds 20 events goes
	// on until a quiet period of 10 minutes.
	Append(ctx context.Context, event NewAuditEvent) (AuditEvent, error)
	// Get returns the event of id, or ErrNotFound.
	Get(ctx context.Context, id int64) (AuditEvent, error)
	// PurgeBefore removes, in one transaction, at most batchSize of the events whose LastAt is before
	// cutoff, oldest first, and returns how many it removed. An aggregated row is kept while its last
	// event is not before cutoff. Removing more events takes more calls.
	PurgeBefore(ctx context.Context, cutoff time.Time, batchSize int64) (int64, error)
}
