package store

// RouteBox is the bounding box of a route, in degrees.
type RouteBox struct {
	MinLat, MinLon, MaxLat, MaxLon float64
}

// WorkoutRoute is the route of a workout (FR-5 of Storage and aggregates), encoded with its metadata
// by internal/workouts/route, which the store does not import.
type WorkoutRoute struct {
	// Encoded is the route in the encoding of ADR 0014, opaque to the store.
	Encoded []byte
	Box     RouteBox
	// Distance is the total distance of the route in m.
	Distance   float64
	PointCount int
	// Polyline is the simplified route for thumbnails, as a Google encoded polyline.
	Polyline string
}

// RouteThumbnail is what the workouts list shows of the route of a workout: its polyline and
// bounding box, without the encoded route.
type RouteThumbnail struct {
	WorkoutID int64
	Box       RouteBox
	Polyline  string
}
