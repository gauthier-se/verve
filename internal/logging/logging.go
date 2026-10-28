// Package logging builds the structured loggers of Verve and carries the request logger in a
// context.
package logging

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/gauthier-se/verve/internal/config"
)

// redacted replaces the value of a sensitive attribute.
const redacted = "[REDACTED]"

// sensitiveKeys mark an attribute as sensitive when its lowercased key contains one of them.
// Lookup table, never modified.
var sensitiveKeys = []string{"password", "passwd", "secret", "token", "authorization", "cookie", "session", "api_key"}

// New returns a logger writing to w in the configured format, from the configured level, with
// the values of sensitive attributes redacted. An unknown level falls back to info and an unknown
// format to text: configuration validation rejects both.
func New(w io.Writer, cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler = slog.NewTextHandler(w, opts)
	if cfg.Format == config.LogFormatJSON {
		h = slog.NewJSONHandler(w, opts)
	}

	return slog.New(redactHandler{next: h})
}

type loggerKey struct{}

// WithContext returns a copy of ctx carrying logger.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext returns the logger stored by WithContext. The request logging middleware stores
// one for every request. Outside requests, loggers are passed explicitly: without a stored
// logger, FromContext returns one that discards everything rather than a global default.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}

	return slog.New(slog.DiscardHandler)
}

// redactHandler replaces the values of sensitive attributes, including inside groups, before
// passing them to the next handler.
type redactHandler struct {
	next slog.Handler
	// all is set inside a group opened by WithGroup under a sensitive name: every value is redacted.
	all bool
}

func (h redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(h.redact(a))

		return true
	})

	return h.next.Handle(ctx, clean)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = h.redact(a)
	}

	return redactHandler{next: h.next.WithAttrs(clean), all: h.all}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{next: h.next.WithGroup(name), all: h.all || isSensitive(name)}
}

func (h redactHandler) redact(a slog.Attr) slog.Attr {
	if h.all {
		return slog.String(a.Key, redacted)
	}

	return redact(a)
}

// redact returns a with its value replaced when its key is sensitive, and the attributes of a
// group redacted recursively. LogValuer values are resolved, unless the key is sensitive.
func redact(a slog.Attr) slog.Attr {
	if isSensitive(a.Key) {
		return slog.String(a.Key, redacted)
	}

	a.Value = a.Value.Resolve()
	if a.Value.Kind() != slog.KindGroup {
		return a
	}

	group := a.Value.Group()
	clean := make([]slog.Attr, len(group))

	for i, member := range group {
		clean[i] = redact(member)
	}

	return slog.Attr{Key: a.Key, Value: slog.GroupValue(clean...)}
}

func isSensitive(key string) bool {
	key = strings.ToLower(key)

	return slices.ContainsFunc(sensitiveKeys, func(s string) bool { return strings.Contains(key, s) })
}
