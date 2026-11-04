package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/store"
)

// AuditEvent is an entry of the audit log.
type AuditEvent struct {
	ID         int64      `json:"id" example:"42" doc:"The id of the event."`
	OccurredAt time.Time  `json:"occurredAt" doc:"When the event occurred, the first of its occurrences for an aggregated event."`
	Type       string     `json:"type" example:"auth.login_failed" doc:"The type of the event."`
	Actor      *AuditUser `json:"actor,omitempty" doc:"The user who acted, left out for the system."`
	Subject    *AuditUser `json:"subject,omitempty" doc:"The user the event is about, left out when there is none."`
	ClientIP   string     `json:"clientIp,omitempty" example:"203.0.113.7" doc:"The IP address of the client, left out outside requests."`
	UserAgent  string     `json:"userAgent,omitempty" example:"Mozilla/5.0 (iPhone)" doc:"The user agent of the client, left out outside requests."`
	// Payload is the JSON object of the constructor of the event in internal/audit.
	Payload json.RawMessage `json:"payload" doc:"The details of the event, an object whose fields depend on its type."`
	Count   int64           `json:"count" example:"1" doc:"The number of occurrences of the event, more than 1 for a burst aggregated into one event."`
	LastAt  time.Time       `json:"lastAt" doc:"When the last occurrence of the event happened, occurredAt unless the event is aggregated."`
}

// AuditUser is a user named by an audit event. Events outlive their users and keep their ids.
type AuditUser struct {
	ID       int64  `json:"id" example:"1" doc:"The id of the user."`
	Username string `json:"username,omitempty" example:"alice" doc:"The username, left out when the user was deleted."`
	Deleted  bool   `json:"deleted" doc:"Whether the user was deleted since the event."`
}

// auditEventsOutput is the response of the audit operations.
type auditEventsOutput struct {
	Body Page[AuditEvent]
}

// adminAuditInput is the input of GET /api/v1/admin/audit.
type adminAuditInput struct {
	PageParams

	Type    string    `query:"type" example:"admin.user_disabled" doc:"Keeps the events of this type."`
	Actor   int64     `query:"actor" minimum:"1" doc:"Keeps the events of the user of this id as actor."`
	Subject int64     `query:"subject" minimum:"1" doc:"Keeps the events about the user of this id."`
	From    time.Time `query:"from" doc:"Keeps the events that occurred at or after this time. An aggregated event is kept while its last occurrence is."`
	To      time.Time `query:"to" doc:"Keeps the events that occurred before this time, which must be after from. An aggregated event is kept when its first occurrence is."`
}

// registerAudit registers GET /api/v1/account/audit, the events of the user of the session, and GET
// /api/v1/admin/audit, every event for an admin, both read in repos.
func registerAudit(api huma.API, repos store.Repositories) {
	huma.Register(api, huma.Operation{
		OperationID: "list-account-audit-events",
		Method:      http.MethodGet,
		Path:        "/api/v1/account/audit",
		Summary:     "List the audit events of the current user",
		Description: "Returns the audit events whose actor or subject is the user of the session, newest first.",
		Tags:        []string{"Audit"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *PageParams) (*auditEventsOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		return listAuditEvents(ctx, repos, store.AuditEventsFilter{UserID: principal.UserID}, *in)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-admin-audit-events",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/audit",
		Summary:     "List the audit events",
		Description: "Returns every audit event, newest first, for an admin. The filters combine.",
		Tags:        []string{"Audit"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *adminAuditInput) (*auditEventsOutput, error) {
		if _, err := requireAdmin(ctx); err != nil {
			return nil, err
		}

		if !in.From.IsZero() && !in.To.IsZero() && !in.To.After(in.From) {
			return nil, huma.NewError(http.StatusUnprocessableEntity, "validation failed",
				&huma.ErrorDetail{Message: "expected a time after from", Location: "query.to", Value: in.To})
		}

		return listAuditEvents(ctx, repos, store.AuditEventsFilter{
			ActorID:   in.Actor,
			SubjectID: in.Subject,
			Type:      in.Type,
			From:      in.From,
			To:        in.To,
		}, in.PageParams)
	})
}

// listAuditEvents returns the page of the events that filter selects that params asks for, read in
// repos.
func listAuditEvents(ctx context.Context, repos store.Repositories, filter store.AuditEventsFilter, params PageParams) (*auditEventsOutput, error) {
	page, err := params.storePage()
	if err != nil {
		return nil, err
	}

	events, err := repos.AuditEvents().List(ctx, filter, page)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}

	events, next := cutPage(params, events, func(event store.AuditEvent) store.Cursor {
		return store.Cursor{At: event.OccurredAt, ID: event.ID}
	})

	users, err := auditUsers(ctx, repos.Users(), events)
	if err != nil {
		return nil, err
	}

	items := make([]AuditEvent, 0, len(events))
	for _, event := range events {
		items = append(items, AuditEvent{
			ID:         event.ID,
			OccurredAt: event.OccurredAt,
			Type:       event.Type,
			Actor:      users[event.ActorID],
			Subject:    users[event.SubjectID],
			ClientIP:   event.ClientIP,
			UserAgent:  event.UserAgent,
			Payload:    event.Payload,
			Count:      event.Count,
			LastAt:     event.LastAt,
		})
	}

	return &auditEventsOutput{Body: Page[AuditEvent]{Items: items, NextCursor: next}}, nil
}

// auditUsers returns the users named by events, by id, looked up in users. A user that users does
// not find was deleted. The system, id 0, is not in the map.
func auditUsers(ctx context.Context, users store.UsersRepository, events []store.AuditEvent) (map[int64]*AuditUser, error) {
	named := make(map[int64]*AuditUser)

	for _, event := range events {
		for _, id := range []int64{event.ActorID, event.SubjectID} {
			if _, ok := named[id]; ok || id == 0 {
				continue
			}

			user, err := users.Get(ctx, id)

			switch {
			case errors.Is(err, store.ErrNotFound):
				named[id] = &AuditUser{ID: id, Deleted: true}
			case err != nil:
				return nil, fmt.Errorf("get user of audit event %d: %w", event.ID, err)
			default:
				named[id] = &AuditUser{ID: id, Username: user.Username}
			}
		}
	}

	return named, nil
}
