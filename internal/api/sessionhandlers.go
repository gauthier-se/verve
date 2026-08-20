package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/route"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// Workout endpoints (ADR 0028). A Session is an entity, not a Metric: it has
// identity, so it is listed, opened and mapped rather than bucketed, and it
// appears on no Panel and in no Series.
//
// The listing carries its own window and its own filters, resolved here rather
// than inherited from a Dashboard: a workout list is browsed by "what did I do",
// and binding it to a Dashboard's range would mean switching Dashboard to find
// last year's race.

const (
	// maxSessionPage caps one page of the workout list, and is also its default.
	maxSessionPage = 50
	// maxRoutePoints caps a Route's drawn polyline. A ride records a point per
	// second, so a long outing is thousands of points of which a line needs a
	// fraction.
	maxRoutePoints = 1000
	// maxProfileSamples caps a Route's elevation and pace profiles. The figures
	// are measured over every point regardless; this bounds only what is sent.
	maxProfileSamples = 500
)

// sessionView is one workout in a listing. Distance and Energy are the Session's
// own totals, which is what the device measured: a figure derived from the
// simplified geometry would disagree with the watch and make every other number
// on the page suspect (ADR 0028).
type sessionView struct {
	ID       int64            `json:"id"`
	Activity catalog.Activity `json:"activity"`
	StartAt  string           `json:"start_at"`
	EndAt    string           `json:"end_at"`
	Duration float64          `json:"duration"`
	Distance *float64         `json:"distance,omitempty"`
	Energy   *float64         `json:"energy,omitempty"`
	Source   string           `json:"source"`
	HasRoute bool             `json:"has_route"`
}

func newSessionView(s data.Session, hasRoute bool) sessionView {
	return sessionView{
		ID:       s.ID,
		Activity: catalog.LookupActivity(s.ActivityType),
		StartAt:  s.StartAt,
		EndAt:    s.EndAt,
		Duration: s.Duration,
		Distance: s.TotalDistance,
		Energy:   s.TotalEnergy,
		Source:   s.Source,
		HasRoute: hasRoute,
	}
}

// sessionTotalsView describes the filter, not the page, and says so: From and To
// are echoed back so a header can name the period it is totalling. A total
// without its domain reads as a truth and is not one, which is why the sleep
// read path puts its Night count on the wire for the same reason.
type sessionTotalsView struct {
	Count    int      `json:"count"`
	Duration float64  `json:"duration"`
	Distance *float64 `json:"distance,omitempty"`
	Energy   *float64 `json:"energy,omitempty"`
	From     string   `json:"from"`
	To       string   `json:"to"`
}

// statView is one Session stat: an aggregate of a canonical Metric over the whole
// workout, carrying the Metric's unit so a client can label it without consulting
// the Catalog.
type statView struct {
	Metric string  `json:"metric"`
	Stat   string  `json:"stat"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
}

// routeRefView is a Route as the detail payload names it: enough to request its
// geometry or download its file, without either being loaded here.
type routeRefView struct {
	ID      int64  `json:"id"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
	Source  string `json:"source"`
}

// routeView is one Route drawn: a simplified polyline and the profiles hanging
// off it. Several Routes on one Session stay several, because joining them would
// draw a line across ground nobody covered (ADR 0028).
type routeView struct {
	ID       int64          `json:"id"`
	StartAt  string         `json:"start_at"`
	EndAt    string         `json:"end_at"`
	Source   string         `json:"source"`
	Points   [][2]float64   `json:"points"` // [lat, lon], simplified
	Profiles route.Profiles `json:"profiles"`
}

// handleListSessions answers the workout list: one page, newest first, plus the
// totals over the whole filter.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	qs := r.URL.Query()
	v := NewValidator()

	// The same range tokens every other window-bearing endpoint takes, so a preset
	// means the same span here as on a Panel even though this page holds its own
	// control. Comparison tokens are deliberately absent: a Baseline overlays two
	// windows of a Series, and a list of entities has nothing to overlay.
	//
	// An absent preset means every workout. A Panel must be told what window to
	// draw; a list of things you did has an obvious default, which is all of them.
	preset := qs.Get("range_preset")
	if preset == "" && qs.Get("range_from") == "" {
		preset = "all"
	}
	resolved, err := timeaxis.Resolve(timeaxis.Tokens{
		RangePreset: preset,
		RangeFrom:   optionalParam(qs, "range_from"),
		RangeTo:     optionalParam(qs, "range_to"),
	}, time.Now())
	if inv, ok := err.(timeaxis.Invalid); ok {
		for field, msg := range inv {
			v.AddError(field, msg)
		}
	} else if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	// The To bound covers the whole of its day, unlike a Series, which stops at
	// the midnight opening it. A Series stops there because the running day is an
	// incomplete bucket and a half-height bar reads as a bad day. A list has no
	// buckets and no such trap, and the workout somebody is most likely looking
	// for is the one they finished an hour ago.
	filter := data.SessionFilter{
		From:       resolved.Current.From.UTC().Format(time.RFC3339),
		To:         resolved.Current.To.UTC().AddDate(0, 0, 1).Format(time.RFC3339),
		Activities: qs["activity"],
		Limit:      maxSessionPage,
	}

	// A group filter is resolved server-side because the groups live in the
	// Catalog: a client enumerating slugs for "all cardio" would silently drop
	// every Activity added after it shipped.
	for _, g := range qs["group"] {
		if !catalog.IsGroup(g) {
			v.AddError("group", fmt.Sprintf("unknown activity group %q", g))
			continue
		}
		slugs, negated := catalog.GroupSlugs(catalog.Group(g))
		if negated {
			filter.ExcludeActs = append(filter.ExcludeActs, slugs...)
		} else {
			filter.Activities = append(filter.Activities, slugs...)
		}
	}

	if raw := qs.Get("cursor"); raw != "" {
		startAt, id, err := decodeSessionCursor(raw)
		if err != nil {
			v.AddError("cursor", "malformed")
		} else {
			filter.CursorStartAt, filter.CursorID = startAt, id
		}
	}

	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	sessions, hasRoute, err := s.models.Sessions.ListSessions(r.Context(), accountID, filter)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	totals, err := s.models.Sessions.SessionTotals(r.Context(), accountID, filter)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	views := make([]sessionView, 0, len(sessions))
	for i, row := range sessions {
		views = append(views, newSessionView(row, hasRoute[i]))
	}

	var next string
	if len(sessions) == filter.Limit {
		last := sessions[len(sessions)-1]
		next = encodeSessionCursor(last.StartAt, last.ID)
	}

	body := envelope{
		"sessions": views,
		"totals": sessionTotalsView{
			Count: totals.Count, Duration: totals.Duration,
			Distance: totals.Distance, Energy: totals.Energy,
			From: filter.From, To: filter.To,
		},
	}
	if next != "" {
		body["next_cursor"] = next
	}
	if err := writeJSON(w, http.StatusOK, body, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleGetSession answers one workout: its figures, its stats and its Route
// references. The geometry is a separate request, so opening a workout does not
// parse a file the caller may not draw.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	session, ok := s.sessionOr404(w, r, accountID)
	if !ok {
		return
	}

	stats, err := s.models.Sessions.SessionStats(r.Context(), accountID, session.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	routes, err := s.models.Sessions.RoutesForSession(r.Context(), accountID, session.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	statViews := make([]statView, 0, len(stats))
	for _, st := range stats {
		unit := ""
		if m, ok := catalog.Lookup(st.Metric); ok {
			unit = m.Unit
		}
		statViews = append(statViews, statView{Metric: st.Metric, Stat: st.Stat, Value: st.Value, Unit: unit})
	}
	routeViews := make([]routeRefView, 0, len(routes))
	for _, rt := range routes {
		routeViews = append(routeViews, routeRefView{ID: rt.ID, StartAt: rt.StartAt, EndAt: rt.EndAt, Source: rt.Source})
	}

	body := envelope{
		"session": newSessionView(session, len(routes) > 0),
		"stats":   statViews,
		"routes":  routeViews,
	}
	if err := writeJSON(w, http.StatusOK, body, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleSessionRoutes answers a workout's geometry: every Route simplified, with
// its profiles, parsed from the stored artifact on demand and cached nowhere.
func (s *Server) handleSessionRoutes(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	session, ok := s.sessionOr404(w, r, accountID)
	if !ok {
		return
	}
	routes, err := s.models.Sessions.RoutesForSession(r.Context(), accountID, session.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	views := make([]routeView, 0, len(routes))
	for _, rt := range routes {
		f, err := os.Open(s.artifactPath(rt.Artifact))
		if err != nil {
			// The row outlives its file only if the data directory was edited by
			// hand. Skip it rather than failing the whole workout: the other
			// segments, the stats and the figures are all still true.
			s.logger.Warn("route artifact missing", "route", rt.ID, "artifact", rt.Artifact, "err", err)
			continue
		}
		track, err := route.Parse(f)
		f.Close()
		if err != nil {
			s.logger.Warn("route artifact unreadable", "route", rt.ID, "err", err)
			continue
		}

		simplified := route.Simplify(track, maxRoutePoints)
		points := make([][2]float64, 0, len(simplified.Points))
		for _, p := range simplified.Points {
			points = append(points, [2]float64{p.Lat, p.Lon})
		}
		views = append(views, routeView{
			ID: rt.ID, StartAt: rt.StartAt, EndAt: rt.EndAt, Source: rt.Source,
			Points:   points,
			Profiles: route.Compute(track, maxProfileSamples),
		})
	}

	if err := writeJSON(w, http.StatusOK, envelope{"routes": views}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleDownloadRoute serves a Route's stored GPX bytes. "Your data is yours"
// does not survive a server that only ever returns its own simplified version.
func (s *Server) handleDownloadRoute(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	session, ok := s.sessionOr404(w, r, accountID)
	if !ok {
		return
	}
	routeID, err := strconv.ParseInt(strings.TrimSuffix(r.PathValue("routeID"), ".gpx"), 10, 64)
	if err != nil {
		s.notFoundResponse(w, r, "route not found")
		return
	}

	routes, err := s.models.Sessions.RoutesForSession(r.Context(), accountID, session.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	for _, rt := range routes {
		if rt.ID != routeID {
			continue
		}
		f, err := os.Open(s.artifactPath(rt.Artifact))
		if err != nil {
			s.notFoundResponse(w, r, "route file not found")
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/gpx+xml")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("route-%d.gpx", rt.ID)))
		http.ServeContent(w, r, "", time.Time{}, f)
		return
	}
	s.notFoundResponse(w, r, "route not found")
}

// artifactPath resolves an artifact name under the artifacts directory. The name
// is content-addressed and cannot hold a separator today; filepath.Base costs one
// call and keeps that from becoming a traversal the day it can.
func (s *Server) artifactPath(artifact string) string {
	return filepath.Join(s.artifactsDir, filepath.Base(artifact))
}

// sessionOr404 resolves the {id} path value to a Session this Account owns.
// Another Account's id is a 404 and never a 403: whether that id exists is not
// this Account's business (ADR 0007).
func (s *Server) sessionOr404(w http.ResponseWriter, r *http.Request, accountID int64) (data.Session, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFoundResponse(w, r, "workout not found")
		return data.Session{}, false
	}
	session, err := s.models.Sessions.GetSession(r.Context(), accountID, id)
	if errors.Is(err, data.ErrRecordNotFound) {
		s.notFoundResponse(w, r, "workout not found")
		return data.Session{}, false
	}
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return data.Session{}, false
	}
	return session, true
}

// encodeSessionCursor packs the keyset a page ends on. It is opaque on purpose:
// a client that cannot take it apart cannot come to depend on the ordering it
// encodes.
func encodeSessionCursor(startAt string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(startAt + "|" + strconv.FormatInt(id, 10)))
}

func decodeSessionCursor(raw string) (string, int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", 0, err
	}
	startAt, rawID, ok := strings.Cut(string(decoded), "|")
	if !ok {
		return "", 0, errors.New("api: cursor has no id")
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return "", 0, err
	}
	return startAt, id, nil
}
