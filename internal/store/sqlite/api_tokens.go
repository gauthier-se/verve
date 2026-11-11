package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// scopeSeparator separates the scopes of a token in the scopes column.
const scopeSeparator = " "

// apiTokens implements store.APITokensRepository. Its errors never hold a hash: the hash of a token
// stands for the token in the database.
type apiTokens struct {
	q   queries
	clk clock.Clock
}

func (r apiTokens) Create(ctx context.Context, token store.NewAPIToken) (store.APIToken, error) {
	if !validScopes(token.Scopes) {
		return store.APIToken{}, fmt.Errorf("create token of user %d: scopes: %w", token.UserID, store.ErrInvalid)
	}

	var row sqlcgen.ApiToken

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.CreateAPIToken(ctx, sqlcgen.CreateAPITokenParams{
			UserID:    token.UserID,
			Name:      token.Name,
			Scopes:    joinScopes(token.Scopes),
			Prefix:    token.Prefix,
			TokenHash: token.TokenHash,
			ExpiresAt: sql.NullInt64{Int64: toMillis(token.ExpiresAt), Valid: !token.ExpiresAt.IsZero()},
			CreatedAt: toMillis(r.clk.Now()),
		})

		// The only foreign key of a token is its user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		return constraintError(err)
	})
	if err != nil {
		return store.APIToken{}, fmt.Errorf("create token of user %d: %w", token.UserID, err)
	}

	return apiTokenOf(row), nil
}

func (r apiTokens) GetByHash(ctx context.Context, tokenHash []byte) (store.APIToken, store.User, error) {
	row, err := r.q.read().GetAPITokenByHash(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.APIToken{}, store.User{}, fmt.Errorf("get token: %w", err)
	}

	return apiTokenOf(row.ApiToken), userOf(row.User), nil
}

func (r apiTokens) ListByUser(ctx context.Context, userID int64) ([]store.APIToken, error) {
	rows, err := r.q.read().ListUserAPITokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list tokens of user %d: %w", userID, err)
	}

	list := make([]store.APIToken, 0, len(rows))
	for _, row := range rows {
		list = append(list, apiTokenOf(row))
	}

	return list, nil
}

func (r apiTokens) Revoke(ctx context.Context, userID, id int64) (store.APIToken, error) {
	var row sqlcgen.ApiToken

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.RevokeUserAPIToken(ctx, sqlcgen.RevokeUserAPITokenParams{
			RevokedAt: nullMillis(r.clk.Now()),
			ID:        id,
			UserID:    userID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.APIToken{}, fmt.Errorf("revoke token %d of user %d: %w", id, userID, err)
	}

	return apiTokenOf(row), nil
}

func (r apiTokens) Touch(ctx context.Context, id int64, ip string) (store.APIToken, error) {
	var row sqlcgen.ApiToken

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.TouchAPIToken(ctx, sqlcgen.TouchAPITokenParams{
			LastUsedAt: nullMillis(r.clk.Now()),
			LastUsedIp: sql.NullString{String: ip, Valid: ip != ""},
			ID:         id,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.APIToken{}, fmt.Errorf("touch token %d: %w", id, err)
	}

	return apiTokenOf(row), nil
}

func (r apiTokens) CountActiveByUsers(ctx context.Context, userIDs []int64) (map[int64]int64, error) {
	counts := make(map[int64]int64, len(userIDs))
	if len(userIDs) == 0 {
		return counts, nil
	}

	rows, err := r.q.read().CountActiveUserAPITokens(ctx, sqlcgen.CountActiveUserAPITokensParams{
		Now:     nullMillis(r.clk.Now()),
		UserIds: userIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("count active tokens of %d users: %w", len(userIDs), err)
	}

	for _, row := range rows {
		counts[row.UserID] = row.Active
	}

	return counts, nil
}

// validScopes reports whether scopes holds one or more distinct known scopes.
func validScopes(scopes []store.TokenScope) bool {
	if len(scopes) == 0 {
		return false
	}

	for i, scope := range scopes {
		if !scope.Known() || slices.Contains(scopes[:i], scope) {
			return false
		}
	}

	return true
}

func joinScopes(scopes []store.TokenScope) string {
	names := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		names = append(names, string(scope))
	}

	return strings.Join(names, scopeSeparator)
}

func splitScopes(column string) []store.TokenScope {
	names := strings.Split(column, scopeSeparator)

	scopes := make([]store.TokenScope, 0, len(names))
	for _, name := range names {
		scopes = append(scopes, store.TokenScope(name))
	}

	return scopes
}

func apiTokenOf(row sqlcgen.ApiToken) store.APIToken {
	return store.APIToken{
		ID:         row.ID,
		UserID:     row.UserID,
		Name:       row.Name,
		Scopes:     splitScopes(row.Scopes),
		Prefix:     row.Prefix,
		ExpiresAt:  fromNullMillis(row.ExpiresAt),
		CreatedAt:  fromMillis(row.CreatedAt),
		LastUsedAt: fromNullMillis(row.LastUsedAt),
		LastUsedIP: row.LastUsedIp.String,
		RevokedAt:  fromNullMillis(row.RevokedAt),
	}
}
