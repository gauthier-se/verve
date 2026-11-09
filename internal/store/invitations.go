package store

import (
	"context"
	"time"
)

// InvitationKind is what a single-use link creates or changes.
type InvitationKind string

// The kinds of a single-use link.
const (
	// InvitationInvite creates the account of the invitee, who chooses their password.
	InvitationInvite InvitationKind = "invite"
)

// NewInvitation describes a single-use link to create. The store never sees the token of the link,
// only its hash.
type NewInvitation struct {
	Kind InvitationKind
	// TokenHash is the SHA-256 hash of the token of the link.
	TokenHash []byte
	// Username, Email and Role describe the account that an invitation creates. Email is empty for
	// none.
	Username  string
	Email     string
	Role      UserRole
	ExpiresAt time.Time
	// CreatedBy is the user who created the link.
	CreatedBy int64
}

// Invitation is a single-use link, by the hash of its token. Times are in UTC with millisecond
// precision.
type Invitation struct {
	ID       int64
	Kind     InvitationKind
	Username string
	// Email is empty for none.
	Email     string
	Role      UserRole
	ExpiresAt time.Time
	// UsedAt is when the link was used, or zero while it is not.
	UsedAt time.Time
	// UserID is the user that using the link created, or 0 while it is not used.
	UserID int64
	// CreatedBy is the user who created the link, or 0 once that user is deleted.
	CreatedBy int64
	CreatedAt time.Time
}

// InvitationsRepository stores the single-use links, by the hash of their token. It never checks
// their expiry: auth.Invitations does.
type InvitationsRepository interface {
	// Create stores a link with the current time as CreatedAt, and returns it. It returns
	// ErrConflict when another link has the same token hash.
	Create(ctx context.Context, invitation NewInvitation) (Invitation, error)
	// GetByTokenHash returns the link of tokenHash, or ErrNotFound.
	GetByTokenHash(ctx context.Context, tokenHash []byte) (Invitation, error)
	// MarkUsed sets the UsedAt of the unused link of id to the current time and its UserID to
	// userID, or returns ErrNotFound when no unused link has id, so that of concurrent uses, only
	// one succeeds.
	MarkUsed(ctx context.Context, id, userID int64) error
}
