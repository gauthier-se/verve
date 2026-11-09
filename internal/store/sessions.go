package store

import (
	"context"
	"time"
)

// LoginMethod is how the user of a session logged in.
type LoginMethod string

// The login methods.
const (
	LoginPassword LoginMethod = "password"
)

// NewSession describes a session to create. The store never sees the session identifier, only its
// hash.
type NewSession struct {
	// IDHash is the SHA-256 hash of the session identifier.
	IDHash []byte
	// PublicID names the session on the sessions page. It is random, so that nothing derived from
	// the identifier reaches a URL.
	PublicID  string
	UserID    int64
	Method    LoginMethod
	ExpiresAt time.Time
	// ClientIP and UserAgent describe the client that logged in, or are empty when unknown.
	ClientIP  string
	UserAgent string
}

// Session is a server-side session. Times are in UTC with millisecond precision.
type Session struct {
	PublicID   string
	UserID     int64
	Method     LoginMethod
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	// ClientIP and UserAgent describe the client that logged in, or are empty when unknown.
	ClientIP  string
	UserAgent string
}

// SessionsRepository stores the server-side sessions, by the hash of their identifier. It never
// checks their expiry: auth.Sessions does.
type SessionsRepository interface {
	// Create stores a session with the current time as CreatedAt and LastSeenAt, and returns it.
	Create(ctx context.Context, session NewSession) (Session, error)
	// Get returns the session of idHash and its user, or ErrNotFound.
	Get(ctx context.Context, idHash []byte) (Session, User, error)
	// Touch sets the LastSeenAt of the session of idHash to the current time and its ExpiresAt to
	// expiresAt, and returns the session, or returns ErrNotFound.
	Touch(ctx context.Context, idHash []byte, expiresAt time.Time) (Session, error)
	// Delete removes the session of idHash, or returns ErrNotFound.
	Delete(ctx context.Context, idHash []byte) error
	// ListByUser returns the sessions of userID, most recently seen first, or an empty list.
	ListByUser(ctx context.Context, userID int64) ([]Session, error)
	// DeleteByPublicID removes the session of userID whose public id is publicID, or returns
	// ErrNotFound, also when the session is of another user.
	DeleteByPublicID(ctx context.Context, userID int64, publicID string) error
	// DeleteOthers removes the sessions of userID, except the one of keepIDHash, and returns how
	// many it removed.
	DeleteOthers(ctx context.Context, userID int64, keepIDHash []byte) (int64, error)
	// PurgeExpired removes the sessions whose ExpiresAt is before now.
	PurgeExpired(ctx context.Context, now time.Time) error
}
