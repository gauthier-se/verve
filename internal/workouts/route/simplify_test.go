package route_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/workouts/route"
)

// Given a route along the equator whose points deviate by 2.2, 7.4 and 11.1 m from the lines
// between their neighbours, then Simplify keeps the first and last points and those that deviate
// by more than the 5 m tolerance.
func TestSimplify_SmallRoute_KeepsPointsBeyondTolerance(t *testing.T) {
	t.Parallel()

	points := []route.Point{
		at(0, 0, 0),
		at(1, 0.00002, 0.0005), // 2.2 m from the line between points 0 and 2
		at(2, 0, 0.001),        // 7.4 m from the line between points 0 and 3
		at(3, 0.0001, 0.0015),  // 11.1 m from the line between points 0 and 4
		at(4, 0, 0.002),
	}

	simplified, tolerance := route.Simplify(points)
	require.Equal(t, []route.Point{points[0], points[2], points[3], points[4]}, simplified)
	require.InDelta(t, route.Tolerance, tolerance, 0)
}

// Given an empty route, a single point or two points, then Simplify returns them.
func TestSimplify_ShortRoutes_ReturnsThem(t *testing.T) {
	t.Parallel()

	for _, points := range [][]route.Point{nil, {at(0, 0, 0)}, {at(0, 0, 0), at(1, 0.001, 0.001)}} {
		simplified, _ := route.Simplify(points)
		require.Equal(t, points, simplified)
	}
}

// Given a long winding route, then its simplified polyline keeps the first and last points,
// deviates from the route by at most the tolerance reached, and holds at most the cap of points.
func TestSimplify_LongRoute_StaysWithinToleranceAndCap(t *testing.T) {
	t.Parallel()

	points := windingRoute(3600)

	simplified, tolerance := route.Simplify(points)

	require.LessOrEqual(t, len(simplified), route.MaxSimplifiedPoints)
	require.Greater(t, len(simplified), route.MaxSimplifiedPoints/4, "the tolerance doubled too often")
	require.Greater(t, tolerance, route.Tolerance, "the route needs more than the cap at the initial tolerance")
	require.Equal(t, points[0], simplified[0])
	require.Equal(t, points[len(points)-1], simplified[len(simplified)-1])

	exponent := math.Log2(tolerance / route.Tolerance)
	require.InDelta(t, math.Round(exponent), exponent, 1e-9, "the tolerance doubles from its initial value")

	requireWithin(t, points, simplified, tolerance)
}

// requireWithin requires that every point of points lies within tolerance meters of the segment
// of simplified between the kept points around it. simplified is a subsequence of points.
func requireWithin(t *testing.T, points, simplified []route.Point, tolerance float64) {
	t.Helper()

	box, _ := route.Bounds(points)
	project := localPlane((box.MinLat+box.MaxLat)/2, (box.MinLon+box.MaxLon)/2)

	next := 1

	for i, p := range points {
		if next < len(simplified)-1 && p == simplified[next] {
			next++
			continue
		}

		if next == len(simplified) {
			break
		}

		a, b := project(simplified[next-1]), project(simplified[next])
		require.LessOrEqual(t, segmentDistance(project(p), a, b), tolerance, "point %d", i)
	}

	require.Equal(t, len(simplified)-1, next, "simplified is a subsequence of points")
}

// localPlane returns the equirectangular projection in meters around lat0 and lon0.
func localPlane(lat0, lon0 float64) func(route.Point) [2]float64 {
	const radius = 6371008.8

	cos := math.Cos(lat0 * math.Pi / 180)

	return func(p route.Point) [2]float64 {
		return [2]float64{radius * (p.Lon - lon0) * math.Pi / 180 * cos, radius * (p.Lat - lat0) * math.Pi / 180}
	}
}

// segmentDistance returns the distance from p to the segment from a to b.
func segmentDistance(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]

	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((p[0]-a[0])*dx+(p[1]-a[1])*dy)/l))
	}

	return math.Hypot(p[0]-a[0]-t*dx, p[1]-a[1]-t*dy)
}

// Given points, then Polyline returns their Google encoded polyline, at 1e-5 degree. The example
// is the one of the documentation of the format.
func TestPolyline_Points_EncodesGooglePolyline(t *testing.T) {
	t.Parallel()

	points := []route.Point{at(0, 38.5, -120.2), at(1, 40.7, -120.95), at(2, 43.252, -126.453)}

	require.Equal(t, "_p~iF~ps|U_ulLnnqC_mqNvxq`@", route.Polyline(points))
	require.Empty(t, route.Polyline(nil))
}
