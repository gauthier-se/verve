package audit

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/logging"
)

// everyConstructor holds an event of each constructor of the package, by the constructor's name.
func everyConstructor() map[string]Event {
	return map[string]Event{
		"LoginFailed": LoginFailed("alice"),
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

	require.ElementsMatch(t, constructorNames(t), slices.Collect(maps.Keys(everyConstructor())))
}

// Given every event constructor, then no payload key is sensitive for the redaction rules of
// internal/logging, and the payload is a JSON object.
func TestConstructors_PayloadsHaveNoSensitiveKey(t *testing.T) {
	t.Parallel()

	for name, event := range everyConstructor() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, event.eventType)
			require.Empty(t, sensitiveKeys(t, event.payload))

			data, err := json.Marshal(event.payload)
			require.NoError(t, err)
			require.Equal(t, byte('{'), data[0])
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
// has neither actor nor subject.
func TestLoginFailed(t *testing.T) {
	t.Parallel()

	event := LoginFailed("alice")

	require.Equal(t, TypeLoginFailed, event.eventType)
	require.Zero(t, event.actorID)
	require.Zero(t, event.subjectID)

	data, err := json.Marshal(event.payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"attempted":"alice"}`, string(data))
}
