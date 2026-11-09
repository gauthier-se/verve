// Package audit records the audit log. Events are built only by the constructors of this package,
// whose payloads are closed lists of fields, so that no event can carry a secret or a health value.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/gauthier-se/verve/internal/store"
)

// The types of the audit events.
const (
	TypeLoginFailed     = "auth.login_failed"
	TypeLoginSucceeded  = "auth.login_succeeded"
	TypeLogout          = "auth.logout"
	TypePasswordChanged = "auth.password_changed"
	TypeSessionRevoked  = "auth.session_revoked"
	TypeUserCreated     = "user.created"
	TypeUserInvited     = "user.invited"
	TypeUserDisabled    = "user.disabled"
	TypeUserEnabled     = "user.enabled"
	TypeUserRoleChanged = "user.role_changed"
	TypePasswordReset   = "user.password_reset"
)

// Event is an audit event to record, built by a constructor of this package.
type Event struct {
	eventType string
	// actorID is the user who acted, or 0 for the system.
	actorID int64
	// subjectID is the user the event is about, or 0 for none.
	subjectID int64
	// aggregateOnClientIP aggregates the bursts of the event on the client IP of the request, and on
	// the subject when there is one.
	aggregateOnClientIP bool
	// payload is a struct of the constructor, never a free map.
	payload any
}

// maxAttemptedLogin is the number of characters kept of an attempted login, which the client chooses.
const maxAttemptedLogin = 64

type loginFailedPayload struct {
	Attempted string `json:"attempted"`
}

// LoginFailed is a failed login, with the login that was attempted, truncated, about the user of
// userID who has that login, or about no one for 0. Its bursts are aggregated on the client IP
// rather than the login, so that trying many logins that no user has does not escape aggregation,
// and on the user too, so that a burst row never shows a user the attempts on another account.
func LoginFailed(attempted string, userID int64) Event {
	return Event{
		eventType:           TypeLoginFailed,
		subjectID:           userID,
		aggregateOnClientIP: true,
		payload:             loginFailedPayload{Attempted: truncate(attempted, maxAttemptedLogin)},
	}
}

type loginSucceededPayload struct {
	Method store.LoginMethod `json:"method"`
}

// LoginSucceeded is the login of the user of userID, with method.
func LoginSucceeded(userID int64, method store.LoginMethod) Event {
	return Event{
		eventType: TypeLoginSucceeded,
		actorID:   userID,
		payload:   loginSucceededPayload{Method: method},
	}
}

type logoutPayload struct{}

// Logout is the user of userID ending their session.
func Logout(userID int64) Event {
	return Event{
		eventType: TypeLogout,
		actorID:   userID,
		payload:   logoutPayload{},
	}
}

type passwordChangedPayload struct{}

// PasswordChanged is the user of userID changing their password, which revoked their other
// sessions.
func PasswordChanged(userID int64) Event {
	return Event{
		eventType: TypePasswordChanged,
		actorID:   userID,
		payload:   passwordChangedPayload{},
	}
}

type sessionsRevokedPayload struct {
	Revoked int64 `json:"revoked"`
}

// SessionsRevoked is the user of userID revoking some of their sessions at once, revoked being how
// many: one from the sessions page, or all but the current one. Its type is TypeSessionRevoked.
func SessionsRevoked(userID, revoked int64) Event {
	return Event{
		eventType: TypeSessionRevoked,
		actorID:   userID,
		payload:   sessionsRevokedPayload{Revoked: revoked},
	}
}

// UserSource is where a user was created from.
type UserSource string

// The sources of a user.
const (
	// SourceSetup is the setup page, which creates the first admin with the setup token.
	SourceSetup UserSource = "setup"
	// SourceCLI is verve user create.
	SourceCLI UserSource = "cli"
	// SourceInvitation is an invitation, accepted by the invitee.
	SourceInvitation UserSource = "invitation"
)

type userCreatedPayload struct {
	Role   store.UserRole `json:"role"`
	Source UserSource     `json:"source"`
}

// UserCreated is the creation of the user of userID, with role, from source. It has no actor: no
// user acted, since the setup comes before any user, the command line has none, and the invitee of
// an invitation is not a user until it creates them.
func UserCreated(userID int64, role store.UserRole, source UserSource) Event {
	return Event{
		eventType: TypeUserCreated,
		subjectID: userID,
		payload:   userCreatedPayload{Role: role, Source: source},
	}
}

type userInvitedPayload struct {
	Username string         `json:"username"`
	Role     store.UserRole `json:"role"`
}

// UserInvited is the admin of adminID inviting username with role. It has no subject: the user does
// not exist until the invitation is accepted, which records UserCreated.
func UserInvited(adminID int64, username string, role store.UserRole) Event {
	return Event{
		eventType: TypeUserInvited,
		actorID:   adminID,
		payload:   userInvitedPayload{Username: username, Role: role},
	}
}

type userDisabledPayload struct{}

// UserDisabled is the admin of adminID, or the system for 0, disabling the user of userID, which
// revoked their sessions.
func UserDisabled(adminID, userID int64) Event {
	return Event{
		eventType: TypeUserDisabled,
		actorID:   adminID,
		subjectID: userID,
		payload:   userDisabledPayload{},
	}
}

type userEnabledPayload struct{}

// UserEnabled is the admin of adminID, or the system for 0, enabling the user of userID again.
func UserEnabled(adminID, userID int64) Event {
	return Event{
		eventType: TypeUserEnabled,
		actorID:   adminID,
		subjectID: userID,
		payload:   userEnabledPayload{},
	}
}

type userRoleChangedPayload struct {
	From store.UserRole `json:"from"`
	To   store.UserRole `json:"to"`
}

// UserRoleChanged is the admin of adminID, or the system for 0, changing the role of the user of
// userID from one role to another, which revoked their sessions.
func UserRoleChanged(adminID, userID int64, from, to store.UserRole) Event {
	return Event{
		eventType: TypeUserRoleChanged,
		actorID:   adminID,
		subjectID: userID,
		payload:   userRoleChangedPayload{From: from, To: to},
	}
}

type passwordResetPayload struct{}

// PasswordReset is the admin of adminID, or the system for 0, replacing the password of the user of
// userID, which revoked their sessions.
func PasswordReset(adminID, userID int64) Event {
	return Event{
		eventType: TypePasswordReset,
		actorID:   adminID,
		subjectID: userID,
		payload:   passwordResetPayload{},
	}
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

	// An IP never holds a space, so that the key of a subject cannot be the one of another IP.
	var aggregationKey string

	switch {
	case !event.aggregateOnClientIP || clientIP == "":
	case event.subjectID != 0:
		aggregationKey = clientIP + " user " + strconv.FormatInt(event.subjectID, 10)
	default:
		aggregationKey = clientIP
	}

	_, err = repo.Append(ctx, store.NewAuditEvent{
		ActorID:        event.actorID,
		SubjectID:      event.subjectID,
		Type:           event.eventType,
		ClientIP:       clientIP,
		UserAgent:      meta.UserAgent,
		Payload:        payload,
		AggregationKey: aggregationKey,
	})
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}

	return nil
}
