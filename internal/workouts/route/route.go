// Package route encodes workout routes and computes what is stored with them: bounding box, total
// distance and simplified polyline ([ADR 0014]). It does no I/O.
//
// [ADR 0014]: https://github.com/gauthier-se/verve/blob/main/docs/decisions/0014-route-encoding.md
package route

import "time"

// Point is a point of a route, as received. An optional field is nil when the source lacks it.
type Point struct {
	Time time.Time
	// Lat and Lon are in degrees.
	Lat, Lon float64
	// Altitude is in meters.
	Altitude *float64
	// HorizontalAccuracy and VerticalAccuracy are in meters.
	HorizontalAccuracy, VerticalAccuracy *float64
	// Speed is in meters per second.
	Speed *float64
	// Course is in degrees from true north.
	Course *float64
}
