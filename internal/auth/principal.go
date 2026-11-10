package auth

import "github.com/gauthier-se/verve/internal/store"

// Method is how a request was authenticated.
type Method string

// The authentication methods.
const (
	// MethodSession authenticates by the session cookie.
	MethodSession Method = "session"
	// MethodToken authenticates by an API token in Authorization: Bearer.
	MethodToken Method = "token"
)

// Principal is the user on whose behalf a request runs, and how the request was authenticated.
type Principal struct {
	UserID int64
	// Role is the role of the user of a session. It is empty for a token: no token carries the
	// privileges of a role.
	Role   store.UserRole
	Method Method
	// TokenID and Scopes are the id and the scopes of the token of MethodToken, or zero.
	TokenID int64
	Scopes  []store.TokenScope
}
