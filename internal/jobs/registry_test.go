package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// testPayload is the payload of the job kinds of the tests.
type testPayload struct {
	Name string `json:"name"`
}

// Given a registered kind, then its handler runs with the decoded payload, the id and the user of
// the job.
func TestRegistry_RegisteredKind_RunsHandlerWithDecodedPayload(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	var got Job[testPayload]

	kind := Register(reg, "test.kind", KindOptions{}, func(_ context.Context, job Job[testPayload]) error {
		got = job

		return nil
	})

	queued, err := kind.New(testPayload{Name: "first"})
	require.NoError(t, err)
	require.Equal(t, "test.kind", queued.Kind)

	err = reg.run(t.Context(), store.Job{ID: 7, Kind: queued.Kind, Payload: queued.Payload, UserID: 3})
	require.NoError(t, err)
	require.Equal(t, Job[testPayload]{ID: 7, UserID: 3, Payload: testPayload{Name: "first"}}, got)
}

// Given a handler that returns an error, then the registry returns it.
func TestRegistry_HandlerFails_ReturnsItsError(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	errHandler := errors.New("handler failed")

	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return errHandler
	})

	queued, err := kind.New(testPayload{})
	require.NoError(t, err)

	err = reg.run(t.Context(), store.Job{Kind: queued.Kind, Payload: queued.Payload})
	require.ErrorIs(t, err, errHandler)
}

// Given a payload that does not decode into the type of its kind, then the handler does not run
// and the error does not quote the payload.
func TestRegistry_MalformedPayload_DoesNotRunTheHandler(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	called := false

	Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		called = true

		return nil
	})

	err := reg.run(t.Context(), store.Job{Kind: "test.kind", Payload: json.RawMessage(`{"name":5}`)})
	require.EqualError(t, err, "malformed job payload")
	require.False(t, called)
}

// Given a job of a kind that is not registered, then it fails with unknown job kind.
func TestRegistry_UnknownKind_Fails(t *testing.T) {
	t.Parallel()

	err := NewRegistry().run(t.Context(), store.Job{Kind: "test.unknown", Payload: json.RawMessage(`{}`)})
	require.EqualError(t, err, "unknown job kind")
}

// Given a kind registered twice, then the wiring panics, because it is a programming error.
func TestRegister_SameKindTwice_Panics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	handler := func(context.Context, Job[testPayload]) error { return nil }

	Register(reg, "test.kind", KindOptions{}, handler)

	require.PanicsWithValue(t, `job kind "test.kind" registered twice`, func() {
		Register(reg, "test.kind", KindOptions{}, handler)
	})
}

// Given a payload that does not encode, then New returns an error naming the kind.
func TestKind_New_PayloadThatDoesNotEncode_Fails(t *testing.T) {
	t.Parallel()

	kind := Register(NewRegistry(), "test.chan", KindOptions{}, func(context.Context, Job[chan int]) error {
		return nil
	})

	_, err := kind.New(make(chan int))
	require.ErrorContains(t, err, `encode payload of job kind "test.chan"`)
}
