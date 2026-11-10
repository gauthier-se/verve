package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"time"

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

// NewToken is what a user sends to create an API token.
type NewToken struct {
	UserID int64
	Name   string
	Scopes []store.TokenScope
	// ExpiresAt is zero for a token that never expires.
	ExpiresAt time.Time
}

// CreateToken stores a new API token through repos, and returns the token and its record. The token
// is returned once: the store only holds its hash and display prefix. It returns the errors of
// store.APITokensRepository.Create, such as store.ErrInvalid for empty or unknown scopes.
func CreateToken(ctx context.Context, repos store.Repositories, token NewToken) (string, store.APIToken, error) {
	secret := generateToken()

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

// generateToken returns a new API token: vrv_ and tokenRandomLength base62 characters, each drawn
// uniformly from crypto/rand by rejection sampling.
func generateToken() string {
	token := make([]byte, 0, len(tokenMarker)+tokenRandomLength)
	token = append(token, tokenMarker...)

	// About 3% of the bytes are rejected: one batch is nearly always enough.
	batch := make([]byte, tokenRandomLength+8)

	for len(token) < cap(token) {
		// crypto/rand.Read never returns an error.
		_, _ = rand.Read(batch)

		for _, b := range batch {
			if int(b) >= tokenByteLimit || len(token) == cap(token) {
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
