package auth

import "github.com/gauthier-se/verve/internal/store"

// Method is how a request was authenticated.
type Method string

// The authentication methods.
const (
	// MethodSession authenticates by the session cookie.
	MethodSession Method = "session"
)

// Principal is the user on whose behalf a request runs, and how the request was authenticated.
type Principal struct {
	UserID int64
	Role   store.UserRole
	Method Method
}
