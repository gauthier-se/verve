package route

import (
	"math"
	"strings"
)

// Simplification of routes for thumbnails. Changing these constants only affects the routes
// written afterward.
const (
	// Tolerance is the initial tolerance of Simplify in meters.
	Tolerance = 5.0
	// MaxSimplifiedPoints is the cap of points of Simplify.
	MaxSimplifiedPoints = 250
)

// polylineUnits is the precision of a Google encoded polyline, 1e-5 degree.
const polylineUnits = 1e5

// Simplify returns the points of a simplified route, and the tolerance it reached: Ramer-Douglas-
// Peucker on the points projected on a local plane around the center of their bounding box. It
// keeps the first and last points, and every point lies within the tolerance of the segment
// between the kept points around it. While the result has more than MaxSimplifiedPoints, the
// tolerance, starting at Tolerance, doubles.
func Simplify(points []Point) ([]Point, float64) {
	tolerance := Tolerance

	if len(points) <= 2 {
		return points, tolerance
	}

	box, _ := Bounds(points)
	plane := project(points, (box.MinLat+box.MaxLat)/2, (box.MinLon+box.MaxLon)/2)

	keep := douglasPeucker(plane, tolerance)
	for len(keep) > MaxSimplifiedPoints {
		tolerance *= 2
		keep = douglasPeucker(plane, tolerance)
	}

	simplified := make([]Point, len(keep))
	for i, k := range keep {
		simplified[i] = points[k]
	}

	return simplified, tolerance
}

// Polyline returns the Google encoded polyline of the latitudes and longitudes of points, at 1e-5
// degree.
func Polyline(points []Point) string {
	var (
		b                strings.Builder
		prevLat, prevLon int64
	)

	for _, p := range points {
		lat := int64(math.Round(p.Lat * polylineUnits))
		lon := int64(math.Round(p.Lon * polylineUnits))

		writePolylineValue(&b, lat-prevLat)
		writePolylineValue(&b, lon-prevLon)

		prevLat, prevLon = lat, lon
	}

	return b.String()
}

// writePolylineValue writes v as in a Google encoded polyline: zigzag encoded, then in chunks of 5
// bits, least significant first, each chunk but the last with the bit 0x20, plus 63.
func writePolylineValue(b *strings.Builder, v int64) {
	u := uint64(v) << 1 //nolint:gosec // G115: zigzag encoding reinterprets the bits
	if v < 0 {
		u = ^u
	}

	for u >= 0x20 {
		b.WriteByte(byte(0x20|u&0x1f) + 63) //nolint:gosec // G115: 6 bits
		u >>= 5
	}

	b.WriteByte(byte(u) + 63) //nolint:gosec // G115: below 0x20 after the loop
}

// vec is a point of a local plane, in meters.
type vec struct {
	x, y float64
}

// project returns points on the equirectangular projection around lat0 and lon0.
func project(points []Point, lat0, lon0 float64) []vec {
	cos := math.Cos(radians(lat0))
	plane := make([]vec, len(points))

	for i, p := range points {
		plane[i] = vec{x: earthRadius * radians(p.Lon-lon0) * cos, y: earthRadius * radians(p.Lat-lat0)}
	}

	return plane
}

// douglasPeucker returns the indexes, in order, of the points of plane that Ramer-Douglas-Peucker
// keeps at tolerance. plane holds at least two points.
func douglasPeucker(plane []vec, tolerance float64) []int {
	kept := make([]bool, len(plane))
	kept[0], kept[len(plane)-1] = true, true

	// Ranges still to split, iteratively: a recursion would go as deep as the route is long.
	stack := [][2]int{{0, len(plane) - 1}}

	for len(stack) > 0 {
		first, last := stack[len(stack)-1][0], stack[len(stack)-1][1]
		stack = stack[:len(stack)-1]

		farthest, distance := 0, 0.0

		for i := first + 1; i < last; i++ {
			if d := segmentDistance(plane[i], plane[first], plane[last]); d > distance {
				farthest, distance = i, d
			}
		}

		if distance > tolerance {
			kept[farthest] = true
			stack = append(stack, [2]int{first, farthest}, [2]int{farthest, last})
		}
	}

	keep := make([]int, 0, len(plane))

	for i, k := range kept {
		if k {
			keep = append(keep, i)
		}
	}

	return keep
}

// segmentDistance returns the distance from p to the segment from a to b.
func segmentDistance(p, a, b vec) float64 {
	dx, dy := b.x-a.x, b.y-a.y

	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l))
	}

	return math.Hypot(p.x-a.x-t*dx, p.y-a.y-t*dy)
}
