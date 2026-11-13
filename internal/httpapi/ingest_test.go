package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/native"
	"github.com/gauthier-se/verve/internal/store"
)

// ingestPath is the path of the native pushes.
const ingestPath = "/api/v1/ingest"

// ingestTestMaxBody is the largest body that the server of the ingest tests accepts.
const ingestTestMaxBody = 256

// ingestPayload is a payload of the native format, which the request never parses.
const ingestPayload = `{"format":"verve.ingest.v1","client":{"name":"Verve for iOS","version":"1.0.0"},"samples":[]}`

// ingestTest is a server whose inbox is a temporary directory, with the user alice.
type ingestTest struct {
	authTest

	inboxDir    string
	alice       store.User
	aliceCookie string
}

// newIngestTest returns an ingestTest on a fresh store, at authTestNow.
func newIngestTest(t *testing.T) ingestTest {
	t.Helper()

	it := ingestTest{authTest: newAuthTest(t), inboxDir: t.TempDir()}
	it.alice = it.mustCreateUser(t, "alice", store.RoleMember)
	it.aliceCookie = "verve_session=" + it.mustCreateSession(t, it.alice)

	return it
}

// handler returns the handler of the server, which writes the bodies with inbox.
func (it ingestTest) handler(t *testing.T, inbox Inbox) http.Handler {
	t.Helper()

	deps := it.deps(t, "")
	deps.Inbox = inbox
	deps.MaxIngestBody = ingestTestMaxBody

	return NewHandler(deps)
}

// inbox returns the real inbox of the temporary directory.
func (it ingestTest) inbox() *ingest.Inbox {
	return ingest.NewInbox(it.inboxDir, it.clock)
}

// inboxFiles returns the names of the files of the inbox.
func (it ingestTest) inboxFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(it.inboxDir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// requireNothingQueued requires that alice has no import and that no job is queued.
func (it ingestTest) requireNothingQueued(t *testing.T) {
	t.Helper()

	imports, err := it.store.Imports().List(t.Context(), it.alice.ID, store.ImportsFilter{}, store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, imports)

	_, _, err = it.store.Jobs().Claim(t.Context(), time.Minute)
	require.ErrorIs(t, err, store.ErrNotFound, "no job is queued")
}

// readCounter counts the bytes read from r.
type readCounter struct {
	r    io.Reader
	read int
}

func (c *readCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += n

	return n, err
}

// pushRequest returns the request of a push of body, with the session cookie when cookie is not
// empty, as the web app sends it, and with the token in Authorization: Bearer when token is not
// empty.
func pushRequest(t *testing.T, body io.Reader, cookie, token string) *http.Request {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com"+ingestPath, body)
	r.Header.Set("Content-Type", "application/json")

	if cookie != "" {
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Verve-Csrf", "1")
		r.Header.Set("Origin", "http://example.com")
	}

	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}

	return r
}

// push serves the push of body on h, and returns the response.
func push(t *testing.T, h http.Handler, body io.Reader, cookie, token string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, pushRequest(t, body, cookie, token))

	return rec
}

// pushedImport is the import of a push response, as the tests read it.
type pushedImport struct {
	ID        int64             `json:"id"`
	Origin    string            `json:"origin"`
	Status    string            `json:"status"`
	Client    map[string]string `json:"client"`
	TokenName string            `json:"tokenName"`
	CreatedAt time.Time         `json:"createdAt"`
	StartedAt *time.Time        `json:"startedAt"`
	Report    struct {
		Total    map[string]int64            `json:"total"`
		Types    map[string]map[string]int64 `json:"types"`
		Workouts map[string]int64            `json:"workouts"`
		Errors   []string                    `json:"errors"`
	} `json:"report"`
}

// mustPush pushes body on h, requires 202 and returns the import of the response with the stored
// import.
func (it ingestTest) mustPush(t *testing.T, h http.Handler, body, cookie, token string) (pushedImport, store.Import) {
	t.Helper()

	rec := push(t, h, strings.NewReader(body), cookie, token)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var got pushedImport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	stored, err := it.store.Imports().Get(t.Context(), it.alice.ID, got.ID)
	require.NoError(t, err)

	return got, stored
}

// requireQueuedJob requires that the job of imp is the queued import.native job of alice, with the
// serial key of her imports.
func (it ingestTest) requireQueuedJob(t *testing.T, imp store.Import) {
	t.Helper()

	job, _, err := it.store.Jobs().Claim(t.Context(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, native.KindImport, job.Kind)
	require.Equal(t, it.alice.ID, job.UserID)
	require.Equal(t, fmt.Sprintf("import:%d", it.alice.ID), job.SerialKey)
	require.JSONEq(t, fmt.Sprintf(`{"importId": %d}`, imp.ID), string(job.Payload))
}

// Given a session, when it posts a payload to POST /api/v1/ingest, then the body is written whole to
// the inbox, the queued import of origin native and its job import.native are created, and the
// server answers 202 with the import.
func TestIngest_SessionPush_StoresTheBodyAndQueuesTheImport(t *testing.T) {
	t.Parallel()

	it := newIngestTest(t)
	h := it.handler(t, it.inbox())

	got, stored := it.mustPush(t, h, ingestPayload, it.aliceCookie, "")
	require.Positive(t, got.ID)
	require.Equal(t, "native", got.Origin)
	require.Equal(t, "queued", got.Status)
	require.Equal(t, authTestNow, got.CreatedAt)
	require.Nil(t, got.StartedAt)
	require.Nil(t, got.Client, "the request never parses the body")
	require.Empty(t, got.TokenName)
	require.Equal(t, map[string]int64{
		"received": 0, "inserted": 0, "updated": 0, "duplicates": 0, "blocked": 0, "invalid": 0,
		"unsupported": 0, "suspicious": 0, "warnings": 0, "deleted": 0, "notFound": 0,
	}, got.Report.Total)
	require.Empty(t, got.Report.Errors)

	require.Equal(t, store.OriginNative, stored.Origin)
	require.Equal(t, store.ImportQueued, stored.Status)
	require.Zero(t, stored.TokenID)
	require.Equal(t, []string{stored.InboxFile}, it.inboxFiles(t))

	body, err := os.ReadFile(filepath.Join(it.inboxDir, stored.InboxFile))
	require.NoError(t, err)
	require.JSONEq(t, ingestPayload, string(body))
	require.Len(t, body, len(ingestPayload), "the body is stored as received")

	it.requireQueuedJob(t, stored)
}

// Given a token with the ingest scope, when it posts a payload, then the import records the token,
// the response names it, and the body of exactly VERVE_MAX_INGEST_BODY bytes is accepted.
func TestIngest_TokenPush_RecordsTheToken(t *testing.T) {
	t.Parallel()

	it := newIngestTest(t)
	h := it.handler(t, it.inbox())
	token, record := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	body := ingestPayload + strings.Repeat(" ", ingestTestMaxBody-len(ingestPayload))

	got, stored := it.mustPush(t, h, body, "", token)
	require.Equal(t, "sync", got.TokenName)
	require.Equal(t, record.ID, stored.TokenID)
	require.Equal(t, []string{stored.InboxFile}, it.inboxFiles(t))

	it.requireQueuedJob(t, stored)
}

// Given a body larger than VERVE_MAX_INGEST_BODY, whether it declares its length or not, then the
// server answers 413 and leaves no file, no import and no job.
func TestIngest_BodyTooLarge_Answers413AndLeavesNoFile(t *testing.T) {
	t.Parallel()

	for _, declared := range []bool{true, false} {
		t.Run(fmt.Sprintf("length declared %t", declared), func(t *testing.T) {
			t.Parallel()

			it := newIngestTest(t)
			h := it.handler(t, it.inbox())
			token, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

			r := pushRequest(t, strings.NewReader(ingestPayload+strings.Repeat(" ", ingestTestMaxBody)), "", token)
			if !declared {
				r.ContentLength = -1
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			status, code := problemCode(t, rec)
			require.Equal(t, http.StatusRequestEntityTooLarge, status)
			require.Equal(t, "bad_request", code)
			require.Empty(t, it.inboxFiles(t))
			it.requireNothingQueued(t)
		})
	}
}

// Given a token without the ingest scope, then the server answers 403 scope_missing before it
// reads the body, and given no authentication, such as the web app without a session or an unknown
// token, then it answers 401 before it reads the body.
func TestIngest_WithoutTheIngestScope_RefusesBeforeReadingTheBody(t *testing.T) {
	t.Parallel()

	it := newIngestTest(t)
	h := it.handler(t, it.inbox())
	readToken, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeRead)

	tests := []struct {
		name   string
		cookie string
		token  string
		status int
		code   string
	}{
		{name: "token without the ingest scope", token: readToken, status: http.StatusForbidden, code: "scope_missing"},
		{name: "no session", cookie: "theme=dark", status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "unknown token", token: "vrv_" + strings.Repeat("A", 43), status: http.StatusUnauthorized, code: "unauthorized"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &readCounter{r: strings.NewReader(ingestPayload)}

			rec := push(t, h, body, tt.cookie, tt.token)

			status, code := problemCode(t, rec)
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.code, code)
			require.Zero(t, body.read, "the body is never read")
			require.Empty(t, it.inboxFiles(t))
			it.requireNothingQueued(t)
		})
	}
}

// fullInbox is an inbox whose disk is full.
type fullInbox struct{}

func (fullInbox) Write(context.Context, io.Reader) (string, error) {
	return "", fmt.Errorf("write inbox file: %w", syscall.ENOSPC)
}

func (fullInbox) Remove(context.Context, string) error {
	return errors.New("no file to remove")
}

// Given a disk full while the body is written, then the server answers 507 and creates no import
// and no job, so that the client retries later.
func TestIngest_DiskFull_Answers507(t *testing.T) {
	t.Parallel()

	it := newIngestTest(t)
	h := it.handler(t, fullInbox{})
	token, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	rec := push(t, h, strings.NewReader(ingestPayload), "", token)

	status, code := problemCode(t, rec)
	require.Equal(t, http.StatusInsufficientStorage, status)
	require.Equal(t, "internal", code)
	it.requireNothingQueued(t)
}

// Given a push whose import cannot be created, then the server answers 500 and removes the file it
// wrote, so that no file waits for an import that will never come.
func TestIngest_ImportNotCreated_RemovesTheFile(t *testing.T) {
	t.Parallel()

	it := newIngestTest(t)
	token, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	// The user is deleted once the token authenticated the request, so that the import of the push
	// cannot be created.
	h := it.handler(t, &failAfterWrite{inbox: it.inbox(), fail: func() {
		require.NoError(t, it.store.Users().Delete(context.Background(), it.alice.ID))
	}})

	rec := push(t, h, strings.NewReader(ingestPayload), "", token)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Empty(t, it.inboxFiles(t))
}

// failAfterWrite is an inbox that calls fail once it wrote a body.
type failAfterWrite struct {
	inbox *ingest.Inbox
	fail  func()
}

func (f *failAfterWrite) Write(ctx context.Context, body io.Reader) (string, error) {
	name, err := f.inbox.Write(ctx, body)
	if err == nil {
		f.fail()
	}

	return name, err
}

func (f *failAfterWrite) Remove(ctx context.Context, name string) error {
	return f.inbox.Remove(ctx, name)
}

// fullStore is a store whose transactions fail as on a full disk.
type fullStore struct {
	store.Store
}

func (fullStore) WithTx(context.Context, func(context.Context, store.Repositories) error) error {
	return fmt.Errorf("commit transaction: %w", store.ErrFull)
}

// Given a disk that fills up in the transaction that creates the import, then the server answers
// 507 and removes the file it wrote, so that the client retries later.
func TestIngest_DiskFullInTheTransaction_Answers507AndLeavesNoFile(t *testing.T) {
	t.Parallel()

	it := newIngestTest(t)
	token, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	deps := it.deps(t, "")
	deps.Store = fullStore{Store: it.store}
	deps.Inbox = it.inbox()
	deps.MaxIngestBody = ingestTestMaxBody

	rec := push(t, NewHandler(deps), strings.NewReader(ingestPayload), "", token)

	status, code := problemCode(t, rec)
	require.Equal(t, http.StatusInsufficientStorage, status)
	require.Equal(t, "internal", code)
	require.Empty(t, it.inboxFiles(t))
	it.requireNothingQueued(t)
}
