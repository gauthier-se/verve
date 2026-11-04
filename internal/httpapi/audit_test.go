package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// The paths of the audit operations.
const (
	accountAuditPath = "/api/v1/account/audit"
	adminAuditPath   = "/api/v1/admin/audit"
)

// auditTest is an authTest with a member alice, an admin root and their session cookies.
type auditTest struct {
	authTest

	alice, root             store.User
	aliceCookie, rootCookie string
	handler                 http.Handler
}

// newAuditTest returns an auditTest on a fresh store, at authTestNow.
func newAuditTest(t *testing.T) auditTest {
	t.Helper()

	a := auditTest{authTest: newAuthTest(t)}
	a.alice = a.mustCreateUser(t, "alice", store.RoleMember)
	a.root = a.mustCreateUser(t, "root", store.RoleAdmin)
	a.aliceCookie = "verve_session=" + a.mustCreateSession(t, a.alice)
	a.rootCookie = "verve_session=" + a.mustCreateSession(t, a.root)
	a.handler = a.authTest.handler(t, "")

	return a
}

// auditEndpoint is an audit operation and the session cookie of a user who may call it.
type auditEndpoint struct {
	path, cookie string
}

// endpoints returns both audit operations: the account one with the cookie of alice, the admin one
// with the cookie of root.
func (a auditTest) endpoints() []auditEndpoint {
	return []auditEndpoint{
		{path: accountAuditPath, cookie: a.aliceCookie},
		{path: adminAuditPath, cookie: a.rootCookie},
	}
}

// mustAppend appends event to the audit log a minute after the previous one, and returns it.
func (a auditTest) mustAppend(t *testing.T, event store.NewAuditEvent) store.AuditEvent {
	t.Helper()

	a.clock.Advance(time.Minute)

	got, err := a.store.AuditEvents().Append(t.Context(), event)
	require.NoError(t, err)

	return got
}

// mustAppendMany appends count copies of event to the audit log in one transaction, at the same
// time, and returns them.
func (a auditTest) mustAppendMany(t *testing.T, count int, event store.NewAuditEvent) []store.AuditEvent {
	t.Helper()

	appended := make([]store.AuditEvent, 0, count)

	err := a.store.WithTx(t.Context(), func(ctx context.Context, tx store.Repositories) error {
		for range count {
			got, err := tx.AuditEvents().Append(ctx, event)
			if err != nil {
				return err
			}

			appended = append(appended, got)
		}

		return nil
	})
	require.NoError(t, err)

	return appended
}

// auditPage is a page of audit events, as the tests read it.
type auditPage struct {
	Items []struct {
		ID         int64     `json:"id"`
		OccurredAt time.Time `json:"occurredAt"`
		Count      int64     `json:"count"`
		LastAt     time.Time `json:"lastAt"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// ids returns the ids of the events of p, in order.
func (p auditPage) ids() []int64 {
	ids := make([]int64, 0, len(p.Items))
	for _, item := range p.Items {
		ids = append(ids, item.ID)
	}

	return ids
}

// getAuditPage gets target with cookie, checks that it answers 200, and returns the page.
func (a auditTest) getAuditPage(t *testing.T, target, cookie string) auditPage {
	t.Helper()

	rec := getWith(t, a.handler, target, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var page auditPage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))

	return page
}

// idsOf returns the ids of events, in order.
func idsOf(events ...store.AuditEvent) []int64 {
	ids := make([]int64, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}

	return ids
}

// auditLog holds the events of appendAuditLog, a minute apart from 08:01 to 08:05.
type auditLog struct {
	// logout is alice logging out, from her phone.
	logout store.AuditEvent
	// disabled is root disabling alice.
	disabled store.AuditEvent
	// setting is root changing a setting, about no user.
	setting store.AuditEvent
	// revoked is the system revoking a session of alice.
	revoked store.AuditEvent
	// backup is the system creating a backup, about no user.
	backup store.AuditEvent
}

// appendAuditLog appends the events of an auditLog.
func (a auditTest) appendAuditLog(t *testing.T) auditLog {
	t.Helper()

	return auditLog{
		logout: a.mustAppend(t, store.NewAuditEvent{
			ActorID:   a.alice.ID,
			Type:      "auth.logout",
			ClientIP:  "203.0.113.7",
			UserAgent: "Mozilla/5.0 (iPhone)",
			Payload:   json.RawMessage(`{}`),
		}),
		disabled: a.mustAppend(t, store.NewAuditEvent{
			ActorID:   a.root.ID,
			SubjectID: a.alice.ID,
			Type:      "admin.user_disabled",
			Payload:   json.RawMessage(`{"reason":"left"}`),
		}),
		setting: a.mustAppend(t, store.NewAuditEvent{
			ActorID: a.root.ID,
			Type:    "admin.setting_changed",
			Payload: json.RawMessage(`{"key":"registration"}`),
		}),
		revoked: a.mustAppend(t, store.NewAuditEvent{
			SubjectID: a.alice.ID,
			Type:      "auth.session_revoked",
			Payload:   json.RawMessage(`{}`),
		}),
		backup: a.mustAppend(t, store.NewAuditEvent{
			Type:    "system.backup_created",
			Payload: json.RawMessage(`{}`),
		}),
	}
}

// Given a user, then GET /api/v1/account/audit returns the events where they are actor or subject,
// newest first, with their time, type, users, client, payload, count and last time. An admin also
// gets their own events only.
func TestAudit_AccountListsTheEventsOfTheUserNewestFirst(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)
	log := a.appendAuditLog(t)

	rec := getWith(t, a.handler, accountAuditPath, a.aliceCookie)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, fmt.Sprintf(`{"items": [
		{
			"id": %[3]d,
			"occurredAt": "2026-11-04T08:04:00Z",
			"type": "auth.session_revoked",
			"subject": {"id": %[1]d, "username": "alice", "deleted": false},
			"payload": {},
			"count": 1,
			"lastAt": "2026-11-04T08:04:00Z"
		},
		{
			"id": %[4]d,
			"occurredAt": "2026-11-04T08:02:00Z",
			"type": "admin.user_disabled",
			"actor": {"id": %[2]d, "username": "root", "deleted": false},
			"subject": {"id": %[1]d, "username": "alice", "deleted": false},
			"payload": {"reason": "left"},
			"count": 1,
			"lastAt": "2026-11-04T08:02:00Z"
		},
		{
			"id": %[5]d,
			"occurredAt": "2026-11-04T08:01:00Z",
			"type": "auth.logout",
			"actor": {"id": %[1]d, "username": "alice", "deleted": false},
			"clientIp": "203.0.113.7",
			"userAgent": "Mozilla/5.0 (iPhone)",
			"payload": {},
			"count": 1,
			"lastAt": "2026-11-04T08:01:00Z"
		}
	]}`, a.alice.ID, a.root.ID, log.revoked.ID, log.disabled.ID, log.logout.ID), rec.Body.String())

	page := a.getAuditPage(t, accountAuditPath, a.rootCookie)
	require.Equal(t, idsOf(log.setting, log.disabled), page.ids())
}

// Given an admin, then GET /api/v1/admin/audit returns every event, newest first, and the filters
// on type, actor, subject and time range keep the matching events. from keeps the events at or
// after it, to the events before it.
func TestAudit_AdminListsEveryEventWithFilters(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)
	log := a.appendAuditLog(t)

	tests := []struct {
		name  string
		query url.Values
		want  []store.AuditEvent
	}{
		{
			name: "none",
			want: []store.AuditEvent{log.backup, log.revoked, log.setting, log.disabled, log.logout},
		},
		{
			name:  "type",
			query: url.Values{"type": {"admin.user_disabled"}},
			want:  []store.AuditEvent{log.disabled},
		},
		{
			name:  "actor",
			query: url.Values{"actor": {fmt.Sprint(a.root.ID)}},
			want:  []store.AuditEvent{log.setting, log.disabled},
		},
		{
			name:  "subject",
			query: url.Values{"subject": {fmt.Sprint(a.alice.ID)}},
			want:  []store.AuditEvent{log.revoked, log.disabled},
		},
		{
			name:  "from",
			query: url.Values{"from": {"2026-11-04T08:02:00Z"}},
			want:  []store.AuditEvent{log.backup, log.revoked, log.setting, log.disabled},
		},
		{
			name:  "to",
			query: url.Values{"to": {"2026-11-04T08:02:00Z"}},
			want:  []store.AuditEvent{log.logout},
		},
		{
			name:  "from and to in another zone",
			query: url.Values{"from": {"2026-11-04T09:02:00+01:00"}, "to": {"2026-11-04T09:04:00+01:00"}},
			want:  []store.AuditEvent{log.setting, log.disabled},
		},
		{
			name: "type, actor and subject",
			query: url.Values{
				"type":    {"admin.user_disabled"},
				"actor":   {fmt.Sprint(a.root.ID)},
				"subject": {fmt.Sprint(a.alice.ID)},
			},
			want: []store.AuditEvent{log.disabled},
		},
		{
			name:  "no match",
			query: url.Values{"type": {"admin.user_disabled"}, "actor": {fmt.Sprint(a.alice.ID)}},
			want:  []store.AuditEvent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			page := a.getAuditPage(t, adminAuditPath+"?"+tt.query.Encode(), a.rootCookie)
			require.Equal(t, idsOf(tt.want...), page.ids())
		})
	}
}

// Given a time range whose end is not after its start, then 422 validation_failed, with to as the
// invalid field.
func TestAudit_AdminRejectsATimeRangeEndingBeforeItStarts(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)

	for _, to := range []string{"2026-11-04T08:00:00Z", "2026-11-04T07:00:00Z"} {
		query := url.Values{"from": {"2026-11-04T08:00:00Z"}, "to": {to}}

		rec := getWith(t, a.handler, adminAuditPath+"?"+query.Encode(), a.rootCookie)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, to)
		require.JSONEq(t, fmt.Sprintf(`{
			"type": "about:blank",
			"title": "Unprocessable Entity",
			"status": 422,
			"detail": "validation failed",
			"code": "validation_failed",
			"errors": [{"message": "expected a time after from", "location": "query.to", "value": %q}]
		}`, to), rec.Body.String(), to)
	}
}

// Given a malformed cursor and a time range ending before it starts, then the 422 lists both
// invalid fields.
func TestAudit_AdminReportsEveryInvalidField(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)
	query := url.Values{"cursor": {"nope"}, "from": {"2026-11-04T08:00:00Z"}, "to": {"2026-11-04T07:00:00Z"}}

	rec := getWith(t, a.handler, adminAuditPath+"?"+query.Encode(), a.rootCookie)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.JSONEq(t, `{
		"type": "about:blank",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"code": "validation_failed",
		"errors": [
			{"message": "invalid cursor", "location": "query.cursor", "value": "nope"},
			{"message": "expected a time after from", "location": "query.to", "value": "2026-11-04T07:00:00Z"}
		]
	}`, rec.Body.String())
}

// Given a member on the admin endpoint, then 403 forbidden. Given no session, then both endpoints
// answer 401 unauthorized.
func TestAudit_MemberOnTheAdminEndpointIsForbidden(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)
	a.appendAuditLog(t)

	rec := getWith(t, a.handler, adminAuditPath, a.aliceCookie)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`, rec.Body.String())

	for _, path := range []string{accountAuditPath, adminAuditPath} {
		rec := getWith(t, a.handler, path)
		require.Equal(t, http.StatusUnauthorized, rec.Code, path)
		require.JSONEq(t, unauthorizedBody, rec.Body.String(), path)
	}
}

// Given more events than the limit, then a page has items and nextCursor: 50 events without limit,
// at most 200 with a larger limit, and the last page has no nextCursor.
func TestAudit_PagesHave50EventsByDefaultAndAtMost200(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)
	a.mustAppendMany(t, 201, store.NewAuditEvent{
		SubjectID: a.alice.ID,
		Type:      "auth.session_revoked",
		Payload:   json.RawMessage(`{}`),
	})

	for _, e := range a.endpoints() {
		t.Run(e.path, func(t *testing.T) {
			t.Parallel()

			page := a.getAuditPage(t, e.path, e.cookie)
			require.Len(t, page.Items, 50)
			require.NotNil(t, page.NextCursor)

			page = a.getAuditPage(t, e.path+"?limit=500", e.cookie)
			require.Len(t, page.Items, 200)
			require.NotNil(t, page.NextCursor)

			page = a.getAuditPage(t, e.path+"?limit=500&cursor="+url.QueryEscape(*page.NextCursor), e.cookie)
			require.Len(t, page.Items, 1)
			require.Nil(t, page.NextCursor)
		})
	}
}

// Given pages followed by their cursor, when new events arrive between two pages, then each event
// is listed once, newest first, and the new events are not: they are newer than the cursor.
func TestAudit_FollowingTheCursorReturnsEachEventOnceWhileEventsArrive(t *testing.T) {
	t.Parallel()

	for _, path := range []string{accountAuditPath, adminAuditPath} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			a := newAuditTest(t)
			aboutAlice := store.NewAuditEvent{SubjectID: a.alice.ID, Type: "auth.session_revoked", Payload: json.RawMessage(`{}`)}

			appended := a.mustAppendMany(t, 3, aboutAlice)
			appended = append(appended, a.mustAppend(t, aboutAlice), a.mustAppend(t, aboutAlice))

			cookie := a.aliceCookie
			if path == adminAuditPath {
				cookie = a.rootCookie
			}

			page := a.getAuditPage(t, path+"?limit=2", cookie)
			ids := page.ids()

			for page.NextCursor != nil {
				a.mustAppend(t, aboutAlice)
				a.mustAppendMany(t, 2, aboutAlice)

				page = a.getAuditPage(t, path+"?limit=2&cursor="+url.QueryEscape(*page.NextCursor), cookie)
				ids = append(ids, page.ids()...)
			}

			require.Equal(t, []int64{appended[4].ID, appended[3].ID, appended[2].ID, appended[1].ID, appended[0].ID}, ids)
		})
	}
}

// Given a malformed cursor, then both endpoints answer 422 validation_failed.
func TestAudit_MalformedCursorReturnsValidationFailed(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)

	for _, e := range a.endpoints() {
		t.Run(e.path, func(t *testing.T) {
			t.Parallel()

			rec := getWith(t, a.handler, e.path+"?cursor=nope", e.cookie)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			require.JSONEq(t, `{
				"type": "about:blank",
				"title": "Unprocessable Entity",
				"status": 422,
				"detail": "validation failed",
				"code": "validation_failed",
				"errors": [{"message": "invalid cursor", "location": "query.cursor", "value": "nope"}]
			}`, rec.Body.String())
		})
	}
}

// Given events whose actor or subject is a user that no longer exists, then they stay listed, with
// that user marked as deleted, without a username.
func TestAudit_EventAboutADeletedUserIsMarked(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)

	byDeleted := a.mustAppend(t, store.NewAuditEvent{
		ActorID:   999,
		SubjectID: a.alice.ID,
		Type:      "admin.user_disabled",
		Payload:   json.RawMessage(`{}`),
	})
	aboutDeleted := a.mustAppend(t, store.NewAuditEvent{
		ActorID:   a.root.ID,
		SubjectID: 998,
		Type:      "account.deletion_completed",
		Payload:   json.RawMessage(`{}`),
	})

	rec := getWith(t, a.handler, adminAuditPath, a.rootCookie)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, fmt.Sprintf(`{"items": [
		{
			"id": %[3]d,
			"occurredAt": "2026-11-04T08:02:00Z",
			"type": "account.deletion_completed",
			"actor": {"id": %[2]d, "username": "root", "deleted": false},
			"subject": {"id": 998, "deleted": true},
			"payload": {},
			"count": 1,
			"lastAt": "2026-11-04T08:02:00Z"
		},
		{
			"id": %[4]d,
			"occurredAt": "2026-11-04T08:01:00Z",
			"type": "admin.user_disabled",
			"actor": {"id": 999, "deleted": true},
			"subject": {"id": %[1]d, "username": "alice", "deleted": false},
			"payload": {},
			"count": 1,
			"lastAt": "2026-11-04T08:01:00Z"
		}
	]}`, a.alice.ID, a.root.ID, aboutDeleted.ID, byDeleted.ID), rec.Body.String())

	page := a.getAuditPage(t, accountAuditPath, a.aliceCookie)
	require.Equal(t, idsOf(byDeleted), page.ids())
}

// Given an aggregated event, then both endpoints return its count and the time of its last
// occurrence, next to the time of its first one.
func TestAudit_AggregatedEventHasCountAndLastAt(t *testing.T) {
	t.Parallel()

	a := newAuditTest(t)

	failedLogin := store.NewAuditEvent{
		SubjectID:      a.alice.ID,
		Type:           "auth.login_failed",
		ClientIP:       "203.0.113.7",
		Payload:        json.RawMessage(`{"attempted":"alice"}`),
		AggregationKey: "203.0.113.7",
	}

	var latest store.AuditEvent

	for range 22 {
		a.clock.Advance(10 * time.Second)

		var err error

		latest, err = a.store.AuditEvents().Append(t.Context(), failedLogin)
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), latest.Count)

	for _, e := range a.endpoints() {
		t.Run(e.path, func(t *testing.T) {
			t.Parallel()

			page := a.getAuditPage(t, e.path+"?limit=1", e.cookie)

			require.Len(t, page.Items, 1)
			require.Equal(t, latest.ID, page.Items[0].ID)
			require.Equal(t, int64(3), page.Items[0].Count)
			require.Equal(t, time.Date(2026, 11, 4, 8, 3, 20, 0, time.UTC), page.Items[0].OccurredAt)
			require.Equal(t, time.Date(2026, 11, 4, 8, 3, 40, 0, time.UTC), page.Items[0].LastAt)
		})
	}
}
