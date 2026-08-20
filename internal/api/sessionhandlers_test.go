package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// seedSession inserts one workout for an Account and returns it.
func seedSession(t *testing.T, models data.Models, accountID int64, activity, startAt string, distance *float64) data.Session {
	return seedSessionN(t, models, accountID, activity, startAt, distance, 0)
}

// seedSessionN seeds a workout whose content key is disambiguated by n, so a test
// can seed many workouts on the same day without the import dedup collapsing them
// into one (which is exactly what it is for, ADR 0006).
func seedSessionN(t *testing.T, models data.Models, accountID int64, activity, startAt string, distance *float64, n int) data.Session {
	t.Helper()
	start, err := time.Parse(time.RFC3339, startAt)
	if err != nil {
		t.Fatalf("parse %s: %v", startAt, err)
	}
	s := data.Session{
		AccountID:     accountID,
		ActivityType:  activity,
		StartAt:       startAt,
		EndAt:         start.Add(time.Hour).UTC().Format(time.RFC3339),
		Duration:      3600,
		TotalDistance: distance,
		Source:        "Apple Watch",
		ContentKey:    data.SessionContentKey(activity, "Apple Watch", startAt, fmt.Sprintf("%s-%d", startAt, n)),
	}
	if _, err := models.Sessions.InsertSession(context.Background(), &s); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return s
}

func accountOf(t *testing.T, models data.Models, email string) int64 {
	t.Helper()
	acc, err := models.Accounts.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("get account %s: %v", email, err)
	}
	return acc.ID
}

func km(v float64) *float64 { return &v }

// sessionList is the decoded shape of GET /v1/sessions.
type sessionList struct {
	Sessions []struct {
		ID       int64 `json:"id"`
		Activity struct {
			Slug    string `json:"slug"`
			Label   string `json:"label"`
			Group   string `json:"group"`
			Reading string `json:"reading"`
		} `json:"activity"`
		StartAt  string   `json:"start_at"`
		Distance *float64 `json:"distance"`
		HasRoute bool     `json:"has_route"`
	} `json:"sessions"`
	Totals struct {
		Count    int      `json:"count"`
		Duration float64  `json:"duration"`
		Distance *float64 `json:"distance"`
		From     string   `json:"from"`
		To       string   `json:"to"`
	} `json:"totals"`
	NextCursor string `json:"next_cursor"`
}

func listSessions(t *testing.T, srv *Server, target string, cookie *http.Cookie) (*http.Response, sessionList) {
	t.Helper()
	res, body := do(t, srv, target, cookie)
	var got sessionList
	if raw, ok := body["sessions"]; ok {
		if err := json.Unmarshal(raw, &got.Sessions); err != nil {
			t.Fatalf("decode sessions: %v", err)
		}
	}
	if raw, ok := body["totals"]; ok {
		if err := json.Unmarshal(raw, &got.Totals); err != nil {
			t.Fatalf("decode totals: %v", err)
		}
	}
	if raw, ok := body["next_cursor"]; ok {
		if err := json.Unmarshal(raw, &got.NextCursor); err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
	}
	return res, got
}

func TestListSessionsNewestFirst(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	seedSession(t, models, acc, "running", "2024-03-01T06:00:00Z", km(5))
	seedSession(t, models, acc, "cycling", "2024-03-03T06:00:00Z", km(30))

	res, got := listSessions(t, srv, "/v1/sessions", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(got.Sessions))
	}
	if got.Sessions[0].StartAt != "2024-03-03T06:00:00Z" {
		t.Errorf("first row = %s, want the newest workout", got.Sessions[0].StartAt)
	}
	// The Activity arrives displayable, so no client has to own the vocabulary.
	if got.Sessions[0].Activity.Label != "Cycling" || got.Sessions[0].Activity.Reading != "speed" {
		t.Errorf("activity = %+v, want Cycling/speed", got.Sessions[0].Activity)
	}
}

// TestListSessionsTotalsDescribeTheFilter is the point of computing totals in
// their own query: a header that folds the returned page instead is one line
// shorter and reports a page sum wearing the clothes of a total.
func TestListSessionsTotalsDescribeTheFilter(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	for i := 0; i < maxSessionPage+10; i++ {
		day := fmt.Sprintf("2024-03-%02dT06:00:00Z", 1+i%28)
		seedSessionN(t, models, acc, "running", day, km(5), i)
	}

	_, got := listSessions(t, srv, "/v1/sessions", cookie)
	if len(got.Sessions) != maxSessionPage {
		t.Fatalf("page = %d, want %d", len(got.Sessions), maxSessionPage)
	}
	if got.Totals.Count != maxSessionPage+10 {
		t.Errorf("totals.count = %d, want %d (the filter, not the page)",
			got.Totals.Count, maxSessionPage+10)
	}
	if got.Totals.Distance == nil || *got.Totals.Distance != float64(maxSessionPage+10)*5 {
		t.Errorf("totals.distance = %v, want the filter's whole distance", got.Totals.Distance)
	}
	// The period is named, so a header can say what it is totalling.
	if got.Totals.From == "" || got.Totals.To == "" {
		t.Error("totals do not name their period")
	}
}

// TestListSessionsCursorPaginates walks every page and asserts the union is the
// whole set exactly once. An offset cursor passes a test with a static table and
// drops or repeats rows the moment an import inserts while someone browses.
func TestListSessionsCursorPaginates(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	const total = maxSessionPage + 20
	for i := 0; i < total; i++ {
		// Deliberately many workouts sharing one start instant, so the cursor is
		// forced to break the tie on id rather than on time alone.
		day := fmt.Sprintf("2024-03-%02dT06:00:00Z", 1+i%5)
		seedSessionN(t, models, acc, "running", day, km(5), i)
	}

	seen := map[int64]int{}
	target := "/v1/sessions"
	for page := 0; page < 10; page++ {
		_, got := listSessions(t, srv, target, cookie)
		for _, row := range got.Sessions {
			seen[row.ID]++
		}
		if got.NextCursor == "" {
			break
		}
		target = "/v1/sessions?cursor=" + got.NextCursor
	}

	if len(seen) != total {
		t.Errorf("saw %d distinct workouts across the pages, want %d", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("workout %d appeared %d times across pages", id, n)
		}
	}
}

func TestListSessionsMalformedCursor(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := do(t, srv, "/v1/sessions?cursor=not-a-cursor", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", res.StatusCode)
	}
}

// TestListSessionsGroupFilter: the group is resolved server-side through the
// Catalog, and Other is a complement so an Activity nobody has curated yet is
// still reachable.
func TestListSessionsGroupFilter(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	seedSession(t, models, acc, "running", "2024-03-01T06:00:00Z", km(5))
	seedSession(t, models, acc, "swimming", "2024-03-02T06:00:00Z", km(1))
	seedSession(t, models, acc, "underwater_basket_weaving", "2024-03-03T06:00:00Z", nil)

	_, cardio := listSessions(t, srv, "/v1/sessions?group=cardio", cookie)
	if len(cardio.Sessions) != 1 || cardio.Sessions[0].Activity.Slug != "running" {
		t.Errorf("cardio = %+v, want the run alone", cardio.Sessions)
	}

	_, other := listSessions(t, srv, "/v1/sessions?group=other", cookie)
	if len(other.Sessions) != 1 || other.Sessions[0].Activity.Slug != "underwater_basket_weaving" {
		t.Errorf("other = %+v, want the uncurated activity", other.Sessions)
	}
	if other.Sessions[0].Activity.Label != "Underwater Basket Weaving" {
		t.Errorf("label = %q, want a prettified fallback", other.Sessions[0].Activity.Label)
	}

	_, byActivity := listSessions(t, srv, "/v1/sessions?activity=swimming", cookie)
	if len(byActivity.Sessions) != 1 || byActivity.Sessions[0].Activity.Slug != "swimming" {
		t.Errorf("activity filter = %+v, want the swim alone", byActivity.Sessions)
	}

	res, _ := do(t, srv, "/v1/sessions?group=nonsense", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown group status = %d, want 422", res.StatusCode)
	}
}

// TestListSessionsIncludesToday: a Series stops at the midnight opening the
// running day, because a partial bucket reads as a bad day. A list has no
// buckets, and the workout somebody is most likely looking for is the one they
// finished an hour ago.
func TestListSessionsIncludesToday(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	today := time.Now().UTC().Truncate(24 * time.Hour).Add(7 * time.Hour)
	seedSession(t, models, acc, "running", today.Format(time.RFC3339), km(5))

	_, got := listSessions(t, srv, "/v1/sessions?range_preset=7d", cookie)
	if len(got.Sessions) != 1 {
		t.Errorf("sessions = %d, want today's workout to be listed", len(got.Sessions))
	}
}

func TestSessionsAreAccountScoped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	other := accountOf(t, models, "other@example.com")
	theirs := seedSession(t, models, other, "running", "2024-03-01T06:00:00Z", km(5))

	_, got := listSessions(t, srv, "/v1/sessions", cookie)
	if len(got.Sessions) != 0 {
		t.Errorf("listed %d of another Account's workouts", len(got.Sessions))
	}

	// A 404 and never a 403: whether that id exists is not this Account's business.
	for _, target := range []string{
		fmt.Sprintf("/v1/sessions/%d", theirs.ID),
		fmt.Sprintf("/v1/sessions/%d/routes", theirs.ID),
		fmt.Sprintf("/v1/sessions/%d/routes/1", theirs.ID),
	} {
		res, _ := do(t, srv, target, cookie)
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", target, res.StatusCode)
		}
	}
}

func TestGetSessionCarriesStats(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	session := seedSession(t, models, acc, "running", "2024-03-01T06:00:00Z", km(10))
	if err := models.Sessions.InsertSessionStats(context.Background(), session.ID, []data.SessionStat{
		{Metric: "heart_rate", Stat: data.StatAverage, Value: 148},
		{Metric: "heart_rate", Stat: data.StatMax, Value: 178},
	}); err != nil {
		t.Fatalf("insert stats: %v", err)
	}

	res, body := do(t, srv, fmt.Sprintf("/v1/sessions/%d", session.ID), cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var stats []struct {
		Metric, Stat, Unit string
		Value              float64
	}
	if err := json.Unmarshal(body["stats"], &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %d, want 2 (an average and a maximum are two answers)", len(stats))
	}
	if stats[0].Unit == "" {
		t.Error("a stat arrived without its unit, so a client cannot label it")
	}
}

// TestSessionWithoutRouteIsNotAnError: a strength session has no trace, and
// asking for its geometry is an empty list rather than a 404.
func TestSessionWithoutRouteIsNotAnError(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	session := seedSession(t, models, acc, "traditional_strength_training", "2024-03-01T06:00:00Z", nil)

	res, body := do(t, srv, fmt.Sprintf("/v1/sessions/%d/routes", session.ID), cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if string(body["routes"]) != "[]" {
		t.Errorf("routes = %s, want an empty array", body["routes"])
	}
}

// seedRoute writes a GPX artifact and the row pointing at it.
func seedRoute(t *testing.T, srv *Server, models data.Models, accountID, sessionID int64, gpx string) data.Route {
	t.Helper()
	if err := os.MkdirAll(srv.artifactsDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	name := fmt.Sprintf("route-%d.gpx", sessionID)
	if err := os.WriteFile(filepath.Join(srv.artifactsDir, name), []byte(gpx), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	r := data.Route{
		AccountID: accountID, SessionID: sessionID, Artifact: name,
		StartAt: "2024-03-01T06:00:00Z", EndAt: "2024-03-01T07:00:00Z",
		Source: "Apple Watch", ContentKey: name,
	}
	if _, err := models.Sessions.InsertRoute(context.Background(), &r); err != nil {
		t.Fatalf("insert route: %v", err)
	}
	return r
}

const testGPX = `<?xml version="1.0"?>
<gpx version="1.1"><trk><trkseg>
 <trkpt lat="48.8566" lon="2.3522"><ele>35</ele><time>2024-03-01T06:00:00Z</time></trkpt>
 <trkpt lat="48.8600" lon="2.3522"><ele>45</ele><time>2024-03-01T06:02:00Z</time></trkpt>
 <trkpt lat="48.8650" lon="2.3522"><ele>60</ele><time>2024-03-01T06:05:00Z</time></trkpt>
</trkseg></trk></gpx>`

func TestSessionRoutesAndDownload(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	session := seedSession(t, models, acc, "running", "2024-03-01T06:00:00Z", km(1))
	seedRoute(t, srv, models, acc, session.ID, testGPX)

	res, body := do(t, srv, fmt.Sprintf("/v1/sessions/%d/routes", session.ID), cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var routes []struct {
		ID       int64        `json:"id"`
		Points   [][2]float64 `json:"points"`
		Profiles struct {
			Samples  []map[string]float64 `json:"samples"`
			LengthKm float64              `json:"length_km"`
			AscentM  float64              `json:"ascent_m"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(body["routes"], &routes); err != nil {
		t.Fatalf("decode routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	if len(routes[0].Points) != 3 {
		t.Errorf("points = %d, want the 3 recorded", len(routes[0].Points))
	}
	if routes[0].Profiles.LengthKm <= 0 {
		t.Error("the profile has no length")
	}
	if routes[0].Profiles.AscentM < 20 {
		t.Errorf("ascent = %v, want ~25 m", routes[0].Profiles.AscentM)
	}

	// The raw file is downloadable: a server that only ever returns its own
	// simplified version does not honour "your data is yours".
	raw := httpGet(t, srv, fmt.Sprintf("/v1/sessions/%d/routes/%d.gpx", session.ID, routes[0].ID), cookie)
	if raw.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", raw.StatusCode)
	}
	if got := raw.Header.Get("Content-Disposition"); got == "" {
		t.Error("the GPX download carries no Content-Disposition")
	}
}

// TestSessionRoutesSurviveAMissingArtifact: a row can outlive its file only if
// the data directory was edited by hand, and when it happens the workout's other
// segments, stats and figures are all still true.
func TestSessionRoutesSurviveAMissingArtifact(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc := accountOf(t, models, testEmail)
	session := seedSession(t, models, acc, "running", "2024-03-01T06:00:00Z", km(1))
	rt := seedRoute(t, srv, models, acc, session.ID, testGPX)
	if err := os.Remove(filepath.Join(srv.artifactsDir, rt.Artifact)); err != nil {
		t.Fatalf("remove artifact: %v", err)
	}

	res, body := do(t, srv, fmt.Sprintf("/v1/sessions/%d/routes", session.ID), cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if string(body["routes"]) != "[]" {
		t.Errorf("routes = %s, want an empty array", body["routes"])
	}
}

// httpGet performs a raw GET, returning the response without decoding a body:
// the GPX download is not JSON.
func httpGet(t *testing.T, srv *Server, target string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

// TestMapConfigReachesTheClient: the workout map draws a basemap only when the
// instance was configured with one, and the client learns that from the payload
// it already loads at boot. Both directions matter and neither is visible from
// inside the running app: with no configuration the page must receive no tile
// URL at all (the default that keeps the browser silent), and with one it must
// receive the URL *and* the attribution the tile source requires.
func TestMapConfigReachesTheClient(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	_, body := do(t, srv, "/v1/auth/me", cookie)
	if _, present := body["map"]; present {
		t.Errorf("an unconfigured instance advertised a basemap: %s", body["map"])
	}

	srv.mapTiles = "https://tiles.example.org/{z}/{x}/{y}.png"
	srv.mapAttrib = "© Example"
	_, body = do(t, srv, "/v1/auth/me", cookie)
	var cfg struct {
		Tiles       string `json:"tiles"`
		Attribution string `json:"attribution"`
	}
	if raw, present := body["map"]; !present {
		t.Fatal("a configured basemap did not reach the client")
	} else if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode map config: %v", err)
	}
	if cfg.Tiles != srv.mapTiles {
		t.Errorf("tiles = %q, want %q", cfg.Tiles, srv.mapTiles)
	}
	if cfg.Attribution != srv.mapAttrib {
		t.Errorf("attribution = %q, want %q: a tile source's credit line is not optional", cfg.Attribution, srv.mapAttrib)
	}
}
