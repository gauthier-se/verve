// Package audit records the audit log. Events are built only by the constructors of this package,
// whose payloads are closed lists of fields, so that no event can carry a secret or a health value.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/store"
)

// The types of the audit events.
const (
	TypeLoginFailed = "auth.login_failed"
)

// Event is an audit event to record, built by a constructor of this package.
type Event struct {
	eventType string
	// actorID is the user who acted, or 0 for the system.
	actorID int64
	// subjectID is the user the event is about, or 0 for none.
	subjectID int64
	// payload is a struct of the constructor, never a free map.
	payload any
}

type loginFailedPayload struct {
	Attempted string `json:"attempted"`
}

// LoginFailed is a failed login, with the login that was attempted.
func LoginFailed(attempted string) Event {
	return Event{eventType: TypeLoginFailed, payload: loginFailedPayload{Attempted: attempted}}
}

// Record appends event to repo, with the client of ctx. For the event to commit with the action it
// describes, repo is tx.AuditEvents() inside the WithTx of that action.
func Record(ctx context.Context, repo store.AuditEventsRepository, event Event) error {
	if event.eventType == "" {
		return errors.New("record audit event: event not built by a constructor")
	}

	payload, err := json.Marshal(event.payload)
	if err != nil {
		return fmt.Errorf("encode audit event of type %q: %w", event.eventType, err)
	}

	meta := MetaFrom(ctx)

	var clientIP string
	if meta.IP.IsValid() {
		clientIP = meta.IP.String()
	}

	_, err = repo.Append(ctx, store.NewAuditEvent{
		ActorID:   event.actorID,
		SubjectID: event.subjectID,
		Type:      event.eventType,
		ClientIP:  clientIP,
		UserAgent: meta.UserAgent,
		Payload:   payload,
	})
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}

	return nil
}
