package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

const (
	// tokenMarker starts every API token, so that secret scanners and people recognize it.
	tokenMarker = "vrv_"
	// tokenAlphabet is base62: the random part of a token survives any header or copy and paste.
	tokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// tokenRandomLength is the number of base62 characters of a token: 43 hold 256.04 bits, at least
	// the 32 random bytes of FR-3 of API tokens.
	tokenRandomLength = 43
	// tokenPrefixLength is the length of the display prefix of a token, such as vrv_3fK9.
	tokenPrefixLength = len(tokenMarker) + 4
	// tokenByteLimit is the largest multiple of 62 up to 256, 248: random bytes from it up are
	// rejected, so that each character is drawn without modulo bias.
	tokenByteLimit = 256 - 256%len(tokenAlphabet)
)

// NewAPIToken is what a user sends to create an API token.
type NewAPIToken struct {
	UserID int64
	Name   string
	Scopes []store.TokenScope
	// ExpiresAt is zero for a token that never expires.
	ExpiresAt time.Time
}

// CreateAPIToken stores a new API token through repos, and returns the token and its record. The
// token is returned once: the store only holds its hash and display prefix. It returns the errors
// of store.APITokensRepository.Create, such as store.ErrInvalid for empty or unknown scopes.
func CreateAPIToken(ctx context.Context, repos store.Repositories, token NewAPIToken) (string, store.APIToken, error) {
	secret := generateAPIToken()

	created, err := repos.APITokens().Create(ctx, store.NewAPIToken{
		UserID:    token.UserID,
		Name:      token.Name,
		Scopes:    token.Scopes,
		Prefix:    secret[:tokenPrefixLength],
		TokenHash: hashAPIToken(secret),
		ExpiresAt: token.ExpiresAt,
	})
	if err != nil {
		return "", store.APIToken{}, err
	}

	return secret, created, nil
}

// generateAPIToken returns a new API token: vrv_ and tokenRandomLength base62 characters, each
// drawn uniformly from crypto/rand by rejection sampling.
func generateAPIToken() string {
	length := len(tokenMarker) + tokenRandomLength

	token := make([]byte, 0, length)
	token = append(token, tokenMarker...)

	// About 3% of the bytes are rejected: one batch is nearly always enough.
	batch := make([]byte, tokenRandomLength+8)

	for len(token) < length {
		// crypto/rand.Read never returns an error.
		_, _ = rand.Read(batch)

		for _, b := range batch {
			if int(b) >= tokenByteLimit || len(token) == length {
				continue
			}

			token = append(token, tokenAlphabet[int(b)%len(tokenAlphabet)])
		}
	}

	return string(token)
}

// hashAPIToken returns the SHA-256 hash of the API token token, as the client sends it.
func hashAPIToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}

// tokenTouchInterval is the shortest time between two writes of the last use of a token, so that
// the requests of a sync do not each write (FR-7 of API tokens).
const tokenTouchInterval = time.Minute

// The rejections of a known API token, whose messages are the codes of their problems: the owner
// of the token can tell them apart (FR-5 of API tokens).
var (
	// ErrTokenRevoked is returned for a revoked token.
	ErrTokenRevoked = errors.New("token_revoked")
	// ErrTokenExpired is returned for a token past its expiry.
	ErrTokenExpired = errors.New("token_expired")
	// ErrUserDisabled is returned for a token of a user who is not active.
	ErrUserDisabled = errors.New("user_disabled")
)

// Tokens authenticates the requests of machine clients by their API token.
type Tokens struct {
	clk clock.Clock
}

// NewTokens returns the tokens service, which takes the current time from clk.
func NewTokens(clk clock.Clock) Tokens {
	return Tokens{clk: clk}
}

// Authenticate returns the API token token, as the client sends it, and its user, read in repos,
// for a request from clientIP, invalid when unknown. It returns store.ErrNotFound for an unknown
// token, and ErrTokenRevoked, ErrTokenExpired or ErrUserDisabled, in that order of precedence,
// for a known token that is rejected, after recording the token.rejected event of the reason. A
// token stays valid until its expiry included.
//
// It writes the last use of a valid token, with clientIP, at most once per minute, and returns the
// token as written.
func (t Tokens) Authenticate(ctx context.Context, repos store.Repositories, token string, clientIP netip.Addr) (store.APIToken, store.User, error) {
	found, user, err := repos.APITokens().GetByHash(ctx, hashAPIToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.APIToken{}, store.User{}, store.ErrNotFound
	}

	if err != nil {
		return store.APIToken{}, store.User{}, err
	}

	now := t.clk.Now()

	var (
		reason   audit.TokenRejection
		rejected error
	)

	switch {
	case !found.RevokedAt.IsZero():
		reason, rejected = audit.TokenRevoked, ErrTokenRevoked
	case !found.ExpiresAt.IsZero() && now.After(found.ExpiresAt):
		reason, rejected = audit.TokenExpired, ErrTokenExpired
	case user.Status != store.UserActive:
		reason, rejected = audit.TokenUserDisabled, ErrUserDisabled
	}

	if rejected != nil {
		if err := audit.Record(ctx, repos.AuditEvents(), audit.TokenRejected(user.ID, found.ID, reason)); err != nil {
			return store.APIToken{}, store.User{}, fmt.Errorf("authenticate token %d: %w", found.ID, err)
		}

		return store.APIToken{}, store.User{}, rejected
	}

	if !found.LastUsedAt.IsZero() && now.Sub(found.LastUsedAt) < tokenTouchInterval {
		return found, user, nil
	}

	// The token is gone when its user was deleted since it was read.
	touched, err := repos.APITokens().Touch(ctx, found.ID, ipText(clientIP))
	if errors.Is(err, store.ErrNotFound) {
		return store.APIToken{}, store.User{}, store.ErrNotFound
	}

	if err != nil {
		return store.APIToken{}, store.User{}, fmt.Errorf("authenticate token %d: %w", found.ID, err)
	}

	return touched, user, nil
}

// LooksLikeAPIToken reports whether s starts like an API token, so that a token sent where it does
// not belong, such as in a URL, is recognized.
func LooksLikeAPIToken(s string) bool {
	return strings.HasPrefix(s, tokenMarker)
}
