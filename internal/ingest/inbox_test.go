package ingest_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// inboxTime is the current time of the inbox tests when they start.
var inboxTime = time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)

// inboxEnv is an inbox in a fresh temporary directory, and a migrated store with a user, both on
// one fake clock.
type inboxEnv struct {
	inbox *ingest.Inbox
	dir   string
	clk   *clock.Fake
	store store.Store
	user  store.User
}

func newInboxEnv(t *testing.T) *inboxEnv {
	t.Helper()

	ctx := t.Context()
	clk := clock.NewFake(inboxTime)

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(ctx, db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clk)
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	dir := t.TempDir()

	return &inboxEnv{inbox: ingest.NewInbox(dir, clk), dir: dir, clk: clk, store: s, user: user}
}

// files returns the names of the entries of the inbox directory.
func (e *inboxEnv) files(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(e.dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// mustWrite writes body to the inbox and returns its name.
func (e *inboxEnv) mustWrite(t *testing.T, body string) string {
	t.Helper()

	name, err := e.inbox.Write(t.Context(), strings.NewReader(body))
	require.NoError(t, err)

	return name
}

// writeAged writes a file of name to the inbox directory, last modified age before the current time.
func (e *inboxEnv) writeAged(t *testing.T, name string, age time.Duration) {
	t.Helper()

	path := filepath.Join(e.dir, name)
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))

	modified := e.clk.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, modified, modified))
}

// mustImport creates a native import of the user whose payload is the inbox file of name, and moves
// it through statuses, in order.
func (e *inboxEnv) mustImport(t *testing.T, name string, statuses ...store.ImportStatus) {
	t.Helper()

	imp, err := e.store.Imports().Create(t.Context(), store.NewImport{UserID: e.user.ID, Origin: store.OriginNative, InboxFile: name})
	require.NoError(t, err)

	for _, status := range statuses {
		_, err = e.store.Imports().SetStatus(t.Context(), e.user.ID, imp.ID, status)
		require.NoError(t, err)
	}
}

// Given a body, when it is written to the inbox, then it is stored whole under a random name that
// Open reads back, and no temporary file is left.
func TestInbox_Write_StoresTheBodyUnderARandomName(t *testing.T) {
	t.Parallel()

	e := newInboxEnv(t)

	first := e.mustWrite(t, `{"format":"verve.ingest.v1"}`)
	second := e.mustWrite(t, `{"format":"verve.ingest.v1"}`)
	require.NotEqual(t, first, second)
	require.ElementsMatch(t, []string{first, second}, e.files(t))

	f, err := e.inbox.Open(t.Context(), first)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	require.JSONEq(t, `{"format":"verve.ingest.v1"}`, string(got))
}

// failingReader yields some bytes, then fails with err.
type failingReader struct {
	read bool
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.err
	}

	r.read = true

	return copy(p, `{"format":`), nil
}

// Given a body whose read fails halfway, such as one larger than its limit, when it is written to
// the inbox, then Write returns the error of the body and leaves no file.
func TestInbox_Write_BodyThatFails_LeavesNoFile(t *testing.T) {
	t.Parallel()

	e := newInboxEnv(t)
	errBody := errors.New("body too large")

	_, err := e.inbox.Write(t.Context(), &failingReader{err: errBody})
	require.ErrorIs(t, err, errBody)
	require.Empty(t, e.files(t))
}

// Given a cancelled context, when a body is written to the inbox, then Write returns the context
// error and leaves no file.
func TestInbox_Write_CancelledContext_LeavesNoFile(t *testing.T) {
	t.Parallel()

	e := newInboxEnv(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := e.inbox.Write(ctx, strings.NewReader(`{}`))
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, e.files(t))
}

// Given the name of a file that is not in the inbox, when it is removed, then Remove succeeds, so
// that removing a file twice is harmless. Given a name that is not the one of an inbox file, when it
// is opened or removed, then it fails and touches nothing outside the inbox.
func TestInbox_Remove_MissingFileSucceedsAndOtherNamesFail(t *testing.T) {
	t.Parallel()

	e := newInboxEnv(t)
	name := e.mustWrite(t, `{}`)

	require.NoError(t, e.inbox.Remove(t.Context(), name))
	require.NoError(t, e.inbox.Remove(t.Context(), name))
	require.Empty(t, e.files(t))

	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte(`{}`), 0o600))

	for _, bad := range []string{"", ".", "..", "../outside", outside, "a/b"} {
		require.Error(t, e.inbox.Remove(t.Context(), bad), "name %q", bad)

		_, err := e.inbox.Open(t.Context(), bad)
		require.Error(t, err, "name %q", bad)
	}

	require.FileExists(t, outside)
}

// Given the daily job cleanup.inbox, when it runs, then it removes the files of imports that failed
// more than 7 days ago and the files without import older than one hour, temporary ones included,
// and keeps the files of queued, running and recently failed imports, and the recent files without
// import, whose import may not be committed yet.
func TestInbox_Cleanup_RemovesTheFilesOfOldFailuresAndOldFilesWithoutImport(t *testing.T) {
	t.Parallel()

	e := newInboxEnv(t)
	ctx := t.Context()

	e.writeAged(t, "failed-8-days-ago", 8*24*time.Hour)
	e.mustImport(t, "failed-8-days-ago", store.ImportRunning, store.ImportFailed)

	e.clk.Advance(2 * 24 * time.Hour)

	e.writeAged(t, "failed-6-days-ago", time.Hour)
	e.mustImport(t, "failed-6-days-ago", store.ImportRunning, store.ImportFailed)

	e.clk.Advance(6 * 24 * time.Hour)

	e.writeAged(t, "queued", 2*time.Hour)
	e.mustImport(t, "queued")
	e.writeAged(t, "running", 2*time.Hour)
	e.mustImport(t, "running", store.ImportRunning)
	e.writeAged(t, "without-import-2-hours-old", 2*time.Hour)
	e.writeAged(t, "without-import-30-minutes-old", 30*time.Minute)
	e.writeAged(t, ".push-temporary-2-hours-old", 2*time.Hour)
	e.writeAged(t, ".push-temporary-30-minutes-old", 30*time.Minute)

	require.NoError(t, e.inbox.Cleanup(ctx, e.store.Imports()))
	require.ElementsMatch(t, []string{
		"failed-6-days-ago",
		"queued",
		"running",
		"without-import-30-minutes-old",
		".push-temporary-30-minutes-old",
	}, e.files(t))
}

// Given the old files of a completed and of a cancelled import, when cleanup.inbox runs, then it
// keeps them: their job removes them, also when it runs again after the process stopped before.
func TestInbox_Cleanup_KeepsTheFilesOfFinishedImports(t *testing.T) {
	t.Parallel()

	e := newInboxEnv(t)

	e.writeAged(t, "completed", 2*time.Hour)
	e.mustImport(t, "completed", store.ImportRunning, store.ImportCompleted)
	e.writeAged(t, "cancelled", 2*time.Hour)
	e.mustImport(t, "cancelled", store.ImportCancelled)

	require.NoError(t, e.inbox.Cleanup(t.Context(), e.store.Imports()))
	require.ElementsMatch(t, []string{"completed", "cancelled"}, e.files(t))
}
