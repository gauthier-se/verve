package applexml

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/ingest"
)

// routes attaches to the workouts of an export the routes of their GPX files (FR-6 of XML import).
type routes struct {
	// zip is nil for a bare export.xml, which has no route.
	zip *zip.Reader
	// dir is the folder of export.xml in the zip, against which a FileReference resolves.
	dir string
	// warned is true once a workout of a bare export.xml carries bareRoutes.
	warned bool
}

// The warnings of a workout whose route is not stored whole (FR-6 of XML import).
const (
	routeMissing      = "route file is missing"
	routeInvalid      = "route file is not a valid GPX file"
	routeNotAvailable = "route is not available"
	bareRoutes        = "routes are not available in a bare export.xml"
)

// attach sets the route of the workout of record, whose WorkoutRoute refers to the GPX file at
// path, a path from the folder of export.xml such as /workout-routes/route_2025-12-30_7.00am.gpx.
// The points outside the time range of the workout are dropped, and a workout whose GPX file is
// missing, is not well-formed XML or holds an unreadable point, or whose export is a bare
// export.xml, has no route: each gets a warning.
//
// It returns an error wrapping ErrMalformed for a corrupted GPX entry, and the error of the file
// when it fails.
func (r *routes) attach(record *ingest.Record, path string) error {
	if r.zip == nil {
		record.Warning = routeNotAvailable

		if !r.warned {
			r.warned = true
			record.Warning += "; " + bareRoutes
		}

		return nil
	}

	f, err := r.zip.Open(r.dir + strings.TrimPrefix(path, "/"))

	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrInvalid):
		record.Warning = routeMissing

		return nil
	case err != nil:
		return fmt.Errorf("open route file: %w", zipError(err))
	}

	defer func() { _ = f.Close() }() // a read-only entry

	w := record.Workout

	start, err := time.Parse(time.RFC3339Nano, w.Start)
	if err != nil {
		return fmt.Errorf("parse workout start: %w", err)
	}

	end, err := time.Parse(time.RFC3339Nano, w.End)
	if err != nil {
		return fmt.Errorf("parse workout end: %w", err)
	}

	points, err := readGPX(entryReader{f})

	switch {
	case errors.Is(err, errInvalidGPX):
		record.Warning = routeInvalid

		return nil
	case err != nil:
		return err
	}

	var dropped int

	for _, p := range points {
		if p.at.Before(start) || p.at.After(end) {
			dropped++

			continue
		}

		w.Route = append(w.Route, p.RoutePoint)
	}

	switch dropped {
	case 0:
	case 1:
		record.Warning = "1 route point outside the workout is dropped"
	default:
		record.Warning = fmt.Sprintf("%d route points outside the workout are dropped", dropped)
	}

	return nil
}

// gpxPoint is a trkpt element of a GPX file, its fields as received.
type gpxPoint struct {
	Lat    string  `xml:"lat,attr"`
	Lon    string  `xml:"lon,attr"`
	Time   string  `xml:"time"`
	Ele    *string `xml:"ele"`
	Speed  *string `xml:"extensions>speed"`
	Course *string `xml:"extensions>course"`
	HAcc   *string `xml:"extensions>hAcc"`
	VAcc   *string `xml:"extensions>vAcc"`
}

// timedPoint is a route point with its instant.
type timedPoint struct {
	ingest.RoutePoint

	at time.Time
}

// errInvalidGPX is the error of a GPX file that is not well-formed XML or holds an unreadable point.
var errInvalidGPX = errors.New("invalid GPX file")

// readGPX returns the points of the GPX file of r in time order, those of the same instant in file
// order. It returns an error wrapping errInvalidGPX for a file that is not well-formed XML or holds
// an unreadable point, and the error of r otherwise.
func readGPX(r io.Reader) ([]timedPoint, error) {
	var points []timedPoint

	dec := xml.NewDecoder(r)

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, gpxError(err)
		}

		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "trkpt" {
			continue
		}

		var p gpxPoint
		if err := dec.DecodeElement(&p, &start); err != nil {
			return nil, gpxError(err)
		}

		point, reason := p.point()
		if reason != "" {
			return nil, fmt.Errorf("%w: route point %d: %s", errInvalidGPX, len(points), reason)
		}

		points = append(points, point)
	}

	slices.SortStableFunc(points, func(a, b timedPoint) int { return a.at.Compare(b.at) })

	return points, nil
}

// point returns the route point of p, or the reason of an unreadable point, which names the field
// and never its value.
func (p gpxPoint) point() (timedPoint, string) {
	at, err := time.Parse(time.RFC3339Nano, p.Time)
	if err != nil {
		return timedPoint{}, "time is not a date"
	}

	lat, ok := parseNumber(p.Lat)
	if !ok {
		return timedPoint{}, "lat is not a number"
	}

	lon, ok := parseNumber(p.Lon)
	if !ok {
		return timedPoint{}, "lon is not a number"
	}

	point := timedPoint{RoutePoint: ingest.RoutePoint{Time: p.Time, Latitude: lat, Longitude: lon}, at: at}

	for _, f := range []struct {
		name  string
		value *string
		field **float64
	}{
		{"ele", p.Ele, &point.Altitude},
		{"speed", p.Speed, &point.Speed},
		{"course", p.Course, &point.Course},
		{"hAcc", p.HAcc, &point.HorizontalAccuracy},
		{"vAcc", p.VAcc, &point.VerticalAccuracy},
	} {
		if f.value == nil {
			continue
		}

		n, ok := parseNumber(*f.value)
		if !ok {
			return timedPoint{}, f.name + " is not a number"
		}

		*f.field = &n
	}

	return point, ""
}

// gpxError returns err, an error of reading a GPX file, as an error of an invalid file, unless it
// is the error of a corrupted entry or a read error of the file.
func gpxError(err error) error {
	if errors.Is(err, ErrMalformed) || errors.As(err, new(diskError)) {
		return fmt.Errorf("read route file: %w", err)
	}

	return fmt.Errorf("%w: %w", errInvalidGPX, err)
}
