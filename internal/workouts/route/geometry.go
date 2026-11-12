package route

import "math"

// earthRadius is the mean radius of the Earth in meters.
const earthRadius = 6371008.8

// Box is the bounding box of a route, in degrees.
type Box struct {
	MinLat, MinLon, MaxLat, MaxLon float64
}

// Bounds returns the bounding box of points, and false when there are none.
func Bounds(points []Point) (Box, bool) {
	if len(points) == 0 {
		return Box{}, false
	}

	box := Box{MinLat: points[0].Lat, MinLon: points[0].Lon, MaxLat: points[0].Lat, MaxLon: points[0].Lon}

	for _, p := range points[1:] {
		box.MinLat = min(box.MinLat, p.Lat)
		box.MinLon = min(box.MinLon, p.Lon)
		box.MaxLat = max(box.MaxLat, p.Lat)
		box.MaxLon = max(box.MaxLon, p.Lon)
	}

	return box, true
}

// Distance returns the total distance of points in meters: the sum of the great circle distances
// between consecutive points, on a sphere of the mean radius of the Earth.
func Distance(points []Point) float64 {
	total := 0.0

	for i := 1; i < len(points); i++ {
		total += greatCircle(points[i-1], points[i])
	}

	return total
}

// greatCircle returns the great circle distance between a and b in meters, with the haversine
// formula, which stays accurate between points a second apart.
func greatCircle(a, b Point) float64 {
	lat1, lat2 := radians(a.Lat), radians(b.Lat)
	sinLat := math.Sin((lat2 - lat1) / 2)
	sinLon := math.Sin(radians(b.Lon-a.Lon) / 2)

	h := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLon*sinLon

	return 2 * earthRadius * math.Asin(math.Sqrt(min(h, 1)))
}

// radians returns deg in radians.
func radians(deg float64) float64 {
	return deg * math.Pi / 180
}
