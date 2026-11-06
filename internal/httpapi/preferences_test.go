package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// preferencesPath is the path of the preferences operations.
const preferencesPath = "/api/v1/account/preferences"

// timezoneInvalidBody is the problem of a timezone that is not an IANA name.
const timezoneInvalidBody = `{"type":"about:blank","title":"Unprocessable Entity","status":422,"code":"timezone_invalid"}`

// patchPreferences sends body as PATCH /api/v1/account/preferences on h, an instance without a public
// URL, as the web app sends it, with the cookies, and returns the response.
func patchPreferences(t *testing.T, h http.Handler, body string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "http://example.com"+preferencesPath,
		strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Verve-Csrf", "1")
	r.Header.Set("Origin", "http://example.com")

	return serveWith(h, r, cookies...)
}

// requireTimezoneBody checks that rec answers 200 with the preferences of timezone, given as JSON.
func requireTimezoneBody(t *testing.T, rec *httptest.ResponseRecorder, timezone string) {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"timezone":`+timezone+`}`, rec.Body.String())
}

// Given a new user, then GET /api/v1/account/preferences returns {"timezone": null}.
func TestPreferences_NewUserHasNoTimezone(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	cookie := "verve_session=" + a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))

	requireTimezoneBody(t, getWith(t, a.handler(t, ""), preferencesPath, cookie), "null")
}

// Given a valid IANA name, then PATCH /api/v1/account/preferences stores it and answers the
// preferences, which GET then returns. Given a body without the timezone, then it is unchanged. The
// other users keep theirs.
func TestPreferences_PatchStoresAValidTimezone(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	alice := "verve_session=" + a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))
	bob := "verve_session=" + a.mustCreateSession(t, a.mustCreateUser(t, "bob", store.RoleMember))

	requireTimezoneBody(t, patchPreferences(t, h, `{"timezone":"Europe/Paris"}`, alice), `"Europe/Paris"`)
	requireTimezoneBody(t, getWith(t, h, preferencesPath, alice), `"Europe/Paris"`)

	requireTimezoneBody(t, patchPreferences(t, h, `{"timezone":"America/New_York"}`, alice), `"America/New_York"`)
	requireTimezoneBody(t, patchPreferences(t, h, `{}`, alice), `"America/New_York"`)
	requireTimezoneBody(t, getWith(t, h, preferencesPath, alice), `"America/New_York"`)

	requireTimezoneBody(t, getWith(t, h, preferencesPath, bob), "null")
}

// Given a name that is not an IANA timezone, then PATCH /api/v1/account/preferences answers 422
// timezone_invalid, and the user keeps the timezone they had.
func TestPreferences_PatchRejectsAnInvalidTimezone(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	cookie := "verve_session=" + a.mustCreateSession(t, a.mustCreateUser(t, "alice", store.RoleMember))

	requireTimezoneBody(t, patchPreferences(t, h, `{"timezone":"Europe/Paris"}`, cookie), `"Europe/Paris"`)

	for _, name := range []string{"Mars/Olympus_Mons", "", "Local", "../../etc/passwd"} {
		rec := patchPreferences(t, h, fmt.Sprintf(`{"timezone":%q}`, name), cookie)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"), name)
		require.JSONEq(t, timezoneInvalidBody, rec.Body.String(), name)
	}

	requireTimezoneBody(t, getWith(t, h, preferencesPath, cookie), `"Europe/Paris"`)
}

// Given no session, then GET and PATCH /api/v1/account/preferences answer 401 unauthorized.
func TestPreferences_RequireASession(t *testing.T) {
	t.Parallel()

	h := newAuthTest(t).handler(t, "")

	for _, rec := range []*httptest.ResponseRecorder{
		getWith(t, h, preferencesPath),
		patchPreferences(t, h, `{"timezone":"Europe/Paris"}`),
	} {
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.JSONEq(t, unauthorizedBody, rec.Body.String())
	}
}

// Given GET /api/v1/auth/me, then it includes the timezone: null for a new user, and the IANA name
// once the user stored one.
func TestPreferences_MeIncludesTheTimezone(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	h := a.handler(t, "")
	user := a.mustCreateUser(t, "alice", store.RoleMember)
	cookie := "verve_session=" + a.mustCreateSession(t, user)
	want := `{"id":%d,"username":"alice","email":"alice@example.com","role":"member","timezone":%s}`

	rec := getWith(t, h, "/api/v1/auth/me", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, fmt.Sprintf(want, user.ID, "null"), rec.Body.String())

	requireTimezoneBody(t, patchPreferences(t, h, `{"timezone":"Asia/Kathmandu"}`, cookie), `"Asia/Kathmandu"`)

	rec = getWith(t, h, "/api/v1/auth/me", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, fmt.Sprintf(want, user.ID, `"Asia/Kathmandu"`), rec.Body.String())
}

// Given a user with a timezone, then the login answers it, as GET /api/v1/auth/me does: the web app
// keeps the user of the login as the user of the session.
func TestPreferences_LoginAnswersTheTimezone(t *testing.T) {
	t.Parallel()

	a := newAuthTest(t)
	alice := a.mustCreateAlice(t)

	_, err := a.store.Preferences().SetTimezone(t.Context(), alice.ID, "Europe/Paris")
	require.NoError(t, err)

	rec := loginAt(t, a.handler(t, ""), phoneAddr, "alice", alicePassword)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"username":"alice","email":"alice@example.com","role":"member",`+
		`"timezone":"Europe/Paris"}`, alice.ID), rec.Body.String())
}
