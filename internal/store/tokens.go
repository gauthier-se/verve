package store

import (
	"context"
	"time"
)

// TokenScope is what an API token may do (FR-2 of API tokens).
type TokenScope string

// The scopes of an API token.
const (
	// ScopeIngest allows the ingestion endpoints only.
	ScopeIngest TokenScope = "ingest"
	// ScopeRead allows the read-only endpoints of the public API, for the data of the token owner.
	ScopeRead TokenScope = "read"
)

// Known reports whether s is one of the scopes of an API token.
func (s TokenScope) Known() bool {
	return s == ScopeIngest || s == ScopeRead
}

// NewAPIToken describes an API token to create. The store never sees the token, only its hash.
type NewAPIToken struct {
	UserID int64
	Name   string
	// Scopes holds one or more distinct scopes.
	Scopes []TokenScope
	// Prefix is the start of the token, shown to tell tokens apart.
	Prefix string
	// TokenHash is the SHA-256 hash of the token.
	TokenHash []byte
	// ExpiresAt is zero for a token that never expires.
	ExpiresAt time.Time
}

// APIToken is a token of a machine client. Times are in UTC with millisecond precision.
type APIToken struct {
	ID     int64
	UserID int64
	Name   string
	Scopes []TokenScope
	Prefix string
	// ExpiresAt is zero for a token that never expires.
	ExpiresAt time.Time
	CreatedAt time.Time
	// LastUsedAt and LastUsedIP describe the last request with the token, or are empty when there
	// was none.
	LastUsedAt time.Time
	LastUsedIP string
	// RevokedAt is when the token was revoked, or zero while it is not.
	RevokedAt time.Time
}

// APITokensRepository stores the API tokens, by the hash of the token. A revoked token stays, so
// that a request with it is told apart from one with an unknown token (FR-5 of API tokens), until
// the deletion of its user. It never checks the expiry or the revocation of a token: auth does.
type APITokensRepository interface {
	// Create stores a token with the current time as CreatedAt, and returns it. It returns
	// ErrConflict when another token has the same hash, ErrNotFound when no user has its UserID,
	// and ErrInvalid when its name is empty, or its scopes empty, repeated or unknown.
	Create(ctx context.Context, token NewAPIToken) (APIToken, error)
	// GetByHash returns the token of tokenHash and its user, also when the token is revoked or
	// expired, or ErrNotFound.
	GetByHash(ctx context.Context, tokenHash []byte) (APIToken, User, error)
	// ListByUser returns the tokens of userID, revoked ones included, newest first, or an empty
	// list.
	ListByUser(ctx context.Context, userID int64) ([]APIToken, error)
	// Revoke sets the RevokedAt of the token of userID whose id is id to the current time, and
	// returns the token. A revoked token keeps its first RevokedAt. It returns ErrNotFound when no
	// token of userID has id, also when the token is of another user.
	Revoke(ctx context.Context, userID, id int64) (APIToken, error)
	// Touch sets the LastUsedAt of the token of id to the current time and its LastUsedIP to ip,
	// empty for an unknown address, and returns the token, or returns ErrNotFound.
	Touch(ctx context.Context, id int64, ip string) (APIToken, error)
	// CountActiveByUsers returns the number of tokens of each of userIDs that are neither revoked nor
	// expired at the current time, a token being valid until its expiry included. The users without
	// such a token are left out.
	CountActiveByUsers(ctx context.Context, userIDs []int64) (map[int64]int64, error)
}
