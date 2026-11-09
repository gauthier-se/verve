package audit

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/logging"
	"github.com/gauthier-se/verve/internal/store"
)

// eventsByConstructor holds an event of each constructor of the package, by the constructor's name.
func eventsByConstructor() map[string]Event {
	return map[string]Event{
		"LoginFailed":     LoginFailed("alice", 7),
		"LoginSucceeded":  LoginSucceeded(7, store.LoginPassword),
		"Logout":          Logout(7),
		"PasswordChanged": PasswordChanged(7),
		"SessionsRevoked": SessionsRevoked(7, 1),
		"UserCreated":     UserCreated(7, store.RoleAdmin, SourceSetup),
	}
}

// constructorNames returns the exported functions of the package that return an Event.
func constructorNames(t *testing.T) []string {
	t.Helper()

	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)

	var names []string

	fset := token.NewFileSet()

	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, source, nil, 0)
		require.NoError(t, err)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.IsExported() && returnsEvent(fn) {
				names = append(names, fn.Name.Name)
			}
		}
	}

	return names
}

func returnsEvent(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}

	return slices.ContainsFunc(fn.Type.Results.List, func(result *ast.Field) bool {
		ident, ok := result.Type.(*ast.Ident)

		return ok && ident.Name == "Event"
	})
}

// sensitiveKeys returns the keys of payload, at any depth, that the redaction rules of
// internal/logging mark as sensitive.
func sensitiveKeys(t *testing.T, payload any) []string {
	t.Helper()

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded any
	require.NoError(t, json.Unmarshal(data, &decoded))

	return collectSensitiveKeys(decoded)
}

func collectSensitiveKeys(value any) []string {
	var keys []string

	switch v := value.(type) {
	case map[string]any:
		for key, member := range v {
			if logging.IsSensitive(key) {
				keys = append(keys, key)
			}

			keys = append(keys, collectSensitiveKeys(member)...)
		}
	case []any:
		for _, member := range v {
			keys = append(keys, collectSensitiveKeys(member)...)
		}
	}

	return keys
}

// Given the table of TestConstructors_PayloadsHaveNoSensitiveKey, then it lists every constructor
// of the package, so that a new constructor cannot skip the check.
func TestConstructors_TableListsEveryConstructor(t *testing.T) {
	t.Parallel()

	require.ElementsMatch(t, constructorNames(t), slices.Collect(maps.Keys(eventsByConstructor())))
}

// Given every event constructor, then no payload key is sensitive for the redaction rules of
// internal/logging, and the payload is a struct, never a free map.
func TestConstructors_PayloadsHaveNoSensitiveKey(t *testing.T) {
	t.Parallel()

	for name, event := range eventsByConstructor() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, event.eventType)
			require.Equal(t, reflect.Struct, reflect.TypeOf(event.payload).Kind())
			require.Empty(t, sensitiveKeys(t, event.payload))
		})
	}
}

// Given a payload with sensitive keys, at the top level or nested, then the check of the
// constructors finds them.
func TestSensitiveKeys_FindsKeysAtAnyDepth(t *testing.T) {
	t.Parallel()

	type nested struct {
		APIKey string `json:"api_key"`
	}

	type payload struct {
		Name     string   `json:"name"`
		Password string   `json:"Password"`
		Nested   nested   `json:"nested"`
		List     []nested `json:"list"`
	}

	got := sensitiveKeys(t, payload{List: []nested{{}}})

	require.ElementsMatch(t, []string{"Password", "api_key", "api_key"}, got)
}

// Given a failed login, then the event has the login failure type, carries the attempted login and
// has no actor. Its subject is the user of the login, or none for a login that no user has.
func TestLoginFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		userID int64
	}{
		{name: "known login", userID: 7},
		{name: "unknown login", userID: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event := LoginFailed("alice", tt.userID)

			require.Equal(t, TypeLoginFailed, event.eventType)
			require.Zero(t, event.actorID)
			require.Equal(t, tt.userID, event.subjectID)

			data, err := json.Marshal(event.payload)
			require.NoError(t, err)
			require.JSONEq(t, `{"attempted":"alice"}`, string(data))
		})
	}
}

// Given failed logins from one client IP, then those of logins that no user has are aggregated on
// the client IP, so that trying many logins does not escape aggregation, and those of a user on the
// client IP and that user, so that a burst row never shows a user the attempts on another account.
// Without a request, they are not aggregated.
func TestLoginFailed_IsAggregatedOnTheClientIPAndTheUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta Meta
		// want holds the aggregation keys of the failures of unknown logins, of user 7 twice and of
		// user 8.
		want []string
	}{
		{
			name: "request",
			meta: Meta{IP: netip.MustParseAddr("203.0.113.7")},
			want: []string{"203.0.113.7", "203.0.113.7", "203.0.113.7 user 7", "203.0.113.7 user 7", "203.0.113.7 user 8"},
		},
		{name: "no request", meta: Meta{}, want: []string{"", "", "", "", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := WithMeta(t.Context(), tt.meta)
			events := &fakeEvents{}

			for _, event := range []Event{
				LoginFailed("bob", 0), LoginFailed("carol", 0),
				LoginFailed("alice", 7), LoginFailed("ALICE@example.com", 7),
				LoginFailed("dave", 8),
			} {
				require.NoError(t, Record(ctx, events, event))
			}

			keys := make([]string, 0, len(events.appended))
			for _, event := range events.appended {
				keys = append(keys, event.AggregationKey)
			}

			require.Equal(t, tt.want, keys)
		})
	}
}

// Given a failed login for an unknown username, then the attempted username is truncated to 64
// characters, not bytes, and the event has no subject.
func TestLoginFailed_TruncatesTheAttemptedLoginTo64Characters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attempted string
		want      string
	}{
		{name: "short", attempted: "alice", want: "alice"},
		{name: "exactly the limit", attempted: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
		{name: "over the limit", attempted: strings.Repeat("a", 65), want: strings.Repeat("a", 64)},
		{name: "multibyte over the limit", attempted: strings.Repeat("é", 100), want: strings.Repeat("é", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event := LoginFailed(tt.attempted, 0)

			require.Zero(t, event.subjectID)
			require.Equal(t, loginFailedPayload{Attempted: tt.want}, event.payload)
		})
	}
}

// Given a successful login, then the event has the login type, the user as actor, no subject, and
// the login method. It is never aggregated.
func TestLoginSucceeded(t *testing.T) {
	t.Parallel()

	event := LoginSucceeded(7, store.LoginPassword)

	require.Equal(t, TypeLoginSucceeded, event.eventType)
	require.Equal(t, int64(7), event.actorID)
	require.Zero(t, event.subjectID)
	require.False(t, event.aggregateOnClientIP)

	data, err := json.Marshal(event.payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"method":"password"}`, string(data))
}

// Given a user created by the setup or by the command line, then the event has the user creation
// type, no actor, since no user acted, the new user as subject, and their role and where they were
// created from. It is never aggregated.
func TestUserCreated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		role   store.UserRole
		source UserSource
		want   string
	}{
		{name: "setup", role: store.RoleAdmin, source: SourceSetup, want: `{"role":"admin","source":"setup"}`},
		{name: "command line", role: store.RoleMember, source: SourceCLI, want: `{"role":"member","source":"cli"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event := UserCreated(7, tt.role, tt.source)

			require.Equal(t, TypeUserCreated, event.eventType)
			require.Equal(t, "user.created", TypeUserCreated)
			require.Zero(t, event.actorID)
			require.Equal(t, int64(7), event.subjectID)
			require.False(t, event.aggregateOnClientIP)

			data, err := json.Marshal(event.payload)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(data))
		})
	}
}

// Given a logout, then the event has the logout type, the user as actor, no subject and an empty
// payload. It is never aggregated.
func TestLogout(t *testing.T) {
	t.Parallel()

	event := Logout(7)

	require.Equal(t, TypeLogout, event.eventType)
	require.Equal(t, int64(7), event.actorID)
	require.Zero(t, event.subjectID)
	require.False(t, event.aggregateOnClientIP)

	data, err := json.Marshal(event.payload)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(data))
}

// Given a password change, then the event has the password change type, the user as actor, no
// subject and an empty payload. It is never aggregated.
func TestPasswordChanged(t *testing.T) {
	t.Parallel()

	event := PasswordChanged(7)

	require.Equal(t, TypePasswordChanged, event.eventType)
	require.Equal(t, "auth.password_changed", TypePasswordChanged)
	require.Equal(t, int64(7), event.actorID)
	require.Zero(t, event.subjectID)
	require.False(t, event.aggregateOnClientIP)

	data, err := json.Marshal(event.payload)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(data))
}

// Given sessions that a user revoked, then the event has the session revocation type, the user as
// actor, no subject, and the number of sessions revoked. It is never aggregated.
func TestSessionsRevoked(t *testing.T) {
	t.Parallel()

	event := SessionsRevoked(7, 3)

	require.Equal(t, TypeSessionRevoked, event.eventType)
	require.Equal(t, "auth.session_revoked", TypeSessionRevoked)
	require.Equal(t, int64(7), event.actorID)
	require.Zero(t, event.subjectID)
	require.False(t, event.aggregateOnClientIP)

	data, err := json.Marshal(event.payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"revoked":3}`, string(data))
}
