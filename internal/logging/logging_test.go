package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/config"
)

// decodeLines decodes each JSON line written by a logger.
func decodeLines(t *testing.T, out string) []map[string]any {
	t.Helper()

	var lines []map[string]any

	for line := range strings.Lines(out) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "line %q", line)

		lines = append(lines, m)
	}

	return lines
}

// Given VERVE_LOG_FORMAT=json, then logs are JSON lines with time, level and msg.
func TestNew_JSONFormatWritesJSONLines(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := New(&buf, config.LogConfig{Level: "info", Format: "json"})
	logger.Info("first", slog.String("user", "alice"))
	logger.Warn("second")

	lines := decodeLines(t, buf.String())
	require.Len(t, lines, 2)
	require.Equal(t, "INFO", lines[0]["level"])
	require.Equal(t, "first", lines[0]["msg"])
	require.Equal(t, "alice", lines[0]["user"])
	require.NotEmpty(t, lines[0]["time"])
	require.Equal(t, "WARN", lines[1]["level"])
	require.Equal(t, "second", lines[1]["msg"])
}

// Given VERVE_LOG_FORMAT=text, then logs use slog's text format.
func TestNew_TextFormatWritesSlogText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := New(&buf, config.LogConfig{Level: "info", Format: "text"})
	logger.Info("hello world", slog.String("user", "alice"))

	require.Regexp(t, `^time=\S+ level=INFO msg="hello world" user=alice\n$`, buf.String())
}

// Given VERVE_LOG_LEVEL, then messages below that level are not emitted.
func TestNew_LevelFiltersMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level string
		want  []string
	}{
		{level: "debug", want: []string{"DEBUG", "INFO", "WARN", "ERROR"}},
		{level: "info", want: []string{"INFO", "WARN", "ERROR"}},
		{level: "warn", want: []string{"WARN", "ERROR"}},
		{level: "error", want: []string{"ERROR"}},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := New(&buf, config.LogConfig{Level: tt.level, Format: "json"})
			logger.Debug("message")
			logger.Info("message")
			logger.Warn("message")
			logger.Error("message")

			var got []string
			for _, line := range decodeLines(t, buf.String()) {
				got = append(got, line["level"].(string))
			}

			require.Equal(t, tt.want, got)
		})
	}
}

// Given a log attribute whose key matches a sensitive name case-insensitively, then its value is
// written as [REDACTED] and the other attributes are kept.
func TestNew_RedactsSensitiveKeys(t *testing.T) {
	t.Parallel()

	sensitive := []string{
		"password", "passwd", "secret", "token", "authorization", "cookie", "session", "api_key",
		"Password", "AUTHORIZATION", "Api_Key",
		"new_password", "session_id", "access_token", "client_secret", "set_cookie",
	}

	var buf bytes.Buffer

	logger := New(&buf, config.LogConfig{Level: "info", Format: "json"})

	attrs := []slog.Attr{slog.String("route", "GET /api/v1/meta"), slog.Int("status", 200)}
	for _, key := range sensitive {
		attrs = append(attrs, slog.String(key, "hunter2"))
	}

	logger.LogAttrs(t.Context(), slog.LevelInfo, "login", attrs...)

	require.NotContains(t, buf.String(), "hunter2")

	line := decodeLines(t, buf.String())[0]
	for _, key := range sensitive {
		require.Equal(t, "[REDACTED]", line[key], key)
	}

	require.Equal(t, "GET /api/v1/meta", line["route"])
	require.InDelta(t, 200, line["status"], 0)
}

// sessionValue is a LogValuer resolving to a group that holds a secret.
type sessionValue struct{ id string }

func (s sessionValue) LogValue() slog.Value {
	return slog.GroupValue(slog.String("user", "alice"), slog.String("session", s.id))
}

// Given sensitive attributes inside groups, added with With or WithGroup, or produced by a
// LogValuer, then their values are written as [REDACTED].
func TestNew_RedactsSensitiveKeysInGroups(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := New(&buf, config.LogConfig{Level: "info", Format: "json"})

	logger.With(slog.String("cookie", "hunter2")).
		WithGroup("auth").
		With(slog.String("token", "hunter2"), slog.String("method", "bearer")).
		Info("request",
			slog.Group("headers", slog.String("Authorization", "Bearer hunter2"), slog.String("accept", "*/*")),
			slog.Group("session", slog.String("id", "hunter2")),
			slog.Any("owner", sessionValue{id: "hunter2"}),
		)

	require.NotContains(t, buf.String(), "hunter2")

	line := decodeLines(t, buf.String())[0]
	require.Equal(t, "[REDACTED]", line["cookie"])

	auth := line["auth"].(map[string]any)
	require.Equal(t, "[REDACTED]", auth["token"])
	require.Equal(t, "bearer", auth["method"])
	require.Equal(t, map[string]any{"Authorization": "[REDACTED]", "accept": "*/*"}, auth["headers"])
	require.Equal(t, "[REDACTED]", auth["session"])
	require.Equal(t, map[string]any{"user": "alice", "session": "[REDACTED]"}, auth["owner"])
}

// Given a group opened with WithGroup under a sensitive name, then every attribute logged in it
// is written as [REDACTED].
func TestNew_RedactsAttributesOfSensitiveGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := New(&buf, config.LogConfig{Level: "info", Format: "json"})

	logger.With(slog.String("user", "alice")).
		WithGroup("session").
		With(slog.String("id", "hunter2")).
		WithGroup("details").
		Info("renewed", slog.String("expires", "hunter2"))

	require.NotContains(t, buf.String(), "hunter2")

	line := decodeLines(t, buf.String())[0]
	require.Equal(t, "alice", line["user"])
	require.Equal(t, map[string]any{
		"id":      "[REDACTED]",
		"details": map[string]any{"expires": "[REDACTED]"},
	}, line["session"])
}

// Given a logger stored with WithContext, then FromContext returns it.
func TestFromContext_ReturnsStoredLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := New(&buf, config.LogConfig{Level: "info", Format: "json"}).With(slog.String("request_id", "abc"))

	FromContext(WithContext(t.Context(), logger)).Info("from context")

	line := decodeLines(t, buf.String())[0]
	require.Equal(t, "abc", line["request_id"])
	require.Equal(t, "from context", line["msg"])
}

// Given a context without logger, then FromContext returns a logger that discards everything
// instead of falling back to a global default.
func TestFromContext_WithoutLoggerDiscards(t *testing.T) {
	t.Parallel()

	logger := FromContext(context.Background())

	require.NotNil(t, logger)
	require.False(t, logger.Enabled(t.Context(), slog.LevelError))
}
