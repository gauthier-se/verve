package audit

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// fakeEvents records the events appended to it. Any other method of the repository panics.
type fakeEvents struct {
	store.AuditEventsRepository

	appended []store.NewAuditEvent
	err      error
}

func (f *fakeEvents) Append(_ context.Context, event store.NewAuditEvent) (store.AuditEvent, error) {
	f.appended = append(f.appended, event)

	return store.AuditEvent{}, f.err
}

// Given a request, then the client IP and the user agent of the event come from the context, and
// the event keeps its actor, subject, type and payload.
func TestRecord_TakesTheClientFromTheContext(t *testing.T) {
	t.Parallel()

	ctx := WithMeta(t.Context(), Meta{IP: netip.MustParseAddr("203.0.113.7"), UserAgent: "curl/8.0"})
	events := &fakeEvents{}
	event := Event{eventType: "test.event", actorID: 7, subjectID: 9, payload: struct {
		Name string `json:"name"`
	}{Name: "backup"}}

	require.NoError(t, Record(ctx, events, event))

	require.Len(t, events.appended, 1)

	got := events.appended[0]
	require.JSONEq(t, `{"name":"backup"}`, string(got.Payload))

	got.Payload = nil
	require.Equal(t, store.NewAuditEvent{
		ActorID:   7,
		SubjectID: 9,
		Type:      "test.event",
		ClientIP:  "203.0.113.7",
		UserAgent: "curl/8.0",
	}, got)
}

// Given a job or a CLI command, whose context has no client, then the client IP and the user agent
// of the event are empty.
func TestRecord_WithoutARequestLeavesTheClientEmpty(t *testing.T) {
	t.Parallel()

	events := &fakeEvents{}

	require.NoError(t, Record(t.Context(), events, LoginFailed("alice", 0)))

	require.Len(t, events.appended, 1)
	require.Empty(t, events.appended[0].ClientIP)
	require.Empty(t, events.appended[0].UserAgent)
}

// Given a repository that fails, then Record returns its error, for the transaction to roll back.
func TestRecord_ReturnsTheErrorOfTheRepository(t *testing.T) {
	t.Parallel()

	errAppend := errors.New("disk full")

	err := Record(t.Context(), &fakeEvents{err: errAppend}, LoginFailed("alice", 0))

	require.ErrorIs(t, err, errAppend)
}

// Given an event that no constructor built, then Record refuses it and appends nothing.
func TestRecord_RefusesAnEventWithoutAConstructor(t *testing.T) {
	t.Parallel()

	events := &fakeEvents{}

	require.Error(t, Record(t.Context(), events, Event{}))
	require.Empty(t, events.appended)
}

// Given a user agent, then it is truncated to 256 characters, not bytes.
func TestWithMeta_TruncatesTheUserAgentTo256Characters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{name: "empty", userAgent: "", want: ""},
		{name: "short", userAgent: "curl/8.0", want: "curl/8.0"},
		{name: "exactly the limit", userAgent: strings.Repeat("a", 256), want: strings.Repeat("a", 256)},
		{name: "over the limit", userAgent: strings.Repeat("a", 257), want: strings.Repeat("a", 256)},
		{name: "multibyte over the limit", userAgent: strings.Repeat("é", 300), want: strings.Repeat("é", 256)},
		{name: "multibyte at the limit", userAgent: strings.Repeat("é", 256), want: strings.Repeat("é", 256)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := WithMeta(t.Context(), Meta{UserAgent: tt.userAgent})

			require.Equal(t, tt.want, MetaFrom(ctx).UserAgent)
		})
	}
}

// Given a context without WithMeta, then MetaFrom returns the zero value.
func TestMetaFrom_WithoutMetaReturnsTheZeroValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, Meta{}, MetaFrom(t.Context()))
}
