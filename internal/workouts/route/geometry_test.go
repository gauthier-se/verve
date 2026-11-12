package route_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/workouts/route"
)

// at returns a point of the route at lat and lon, i seconds after its start.
func at(i int, lat, lon float64) route.Point {
	return route.Point{Time: time.Date(2026, 3, 14, 7, 0, i, 0, time.UTC), Lat: lat, Lon: lon}
}

// Given a route, then its bounding box holds every point, and touches the extreme ones.
func TestBounds_Route_HoldsEveryPoint(t *testing.T) {
	t.Parallel()

	points := []route.Point{at(0, 0.001, -0.002), at(1, -0.003, 0.004), at(2, 0.005, 0.001), at(3, 0, -0.006)}

	box, ok := route.Bounds(points)
	require.True(t, ok)
	require.Equal(t, route.Box{MinLat: -0.003, MinLon: -0.006, MaxLat: 0.005, MaxLon: 0.004}, box)
}

// Given an empty route, then it has no bounding box. Given a single point, then its box is the
// point.
func TestBounds_EmptyRouteAndSinglePoint(t *testing.T) {
	t.Parallel()

	_, ok := route.Bounds(nil)
	require.False(t, ok)

	box, ok := route.Bounds([]route.Point{at(0, 0.5, -0.5)})
	require.True(t, ok)
	require.Equal(t, route.Box{MinLat: 0.5, MinLon: -0.5, MaxLat: 0.5, MaxLon: -0.5}, box)
}

// Given a route, then its total distance is the sum of the great circle distances between
// consecutive points. On the equator and on a meridian, 0.001 degree is 6371008.8 m (mean Earth
// radius) times 0.001 pi / 180, 111.19508 m.
func TestDistance_Route_SumsGreatCircleDistances(t *testing.T) {
	t.Parallel()

	const step = 111.1950802335329 // 6371008.8 * 0.001 * pi / 180

	tests := []struct {
		name   string
		points []route.Point
		want   float64
	}{
		{
			name:   "east along the equator",
			points: []route.Point{at(0, 0, 0), at(1, 0, 0.001), at(2, 0, 0.003)},
			want:   3 * step,
		},
		{
			name:   "north along the prime meridian, then back",
			points: []route.Point{at(0, 0, 0), at(1, 0.002, 0), at(2, 0.001, 0)},
			want:   3 * step,
		},
		{
			name:   "a quarter of the equator",
			points: []route.Point{at(0, 0, 0), at(1, 0, 90)},
			want:   6371008.8 * 3.141592653589793 / 2,
		},
		{
			name:   "standing still",
			points: []route.Point{at(0, 0.001, 0.001), at(1, 0.001, 0.001)},
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.InDelta(t, tt.want, route.Distance(tt.points), 1e-6)
		})
	}
}

// Given an empty route or a single point, then the distance is 0.
func TestDistance_EmptyRouteAndSinglePoint_IsZero(t *testing.T) {
	t.Parallel()

	require.Zero(t, route.Distance(nil))
	require.Zero(t, route.Distance([]route.Point{at(0, 0.5, 0.5)}))
}
