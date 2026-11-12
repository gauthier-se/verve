package route_test

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/gauthier-se/verve/internal/workouts/route"
)

// windingRoute returns an invented run of n points one second apart around latitude 0 and
// longitude 0: a loop of about 3 m/s with a winding path and the noise of a GPS receiver, every
// field present.
func windingRoute(n int) []route.Point {
	rng := rand.New(rand.NewPCG(7, 14)) //nolint:gosec // G404: synthetic data, not a secret
	start := time.Date(2026, 3, 14, 7, 0, 0, 0, time.UTC)

	points := make([]route.Point, n)

	for i := range points {
		angle := 2 * math.Pi * float64(i) / float64(n)
		wind := 0.0008 * math.Sin(17*angle)

		points[i] = route.Point{
			Time:               start.Add(time.Duration(i) * time.Second),
			Lat:                (0.02+wind)*math.Sin(angle) + rng.NormFloat64()*0.00002,
			Lon:                (0.02+wind)*math.Cos(angle) + rng.NormFloat64()*0.00002,
			Altitude:           ptr(100 + 20*math.Sin(3*angle) + rng.NormFloat64()*0.3),
			HorizontalAccuracy: ptr(3 + rng.Float64()*8),
			VerticalAccuracy:   ptr(2 + rng.Float64()*4),
			Speed:              ptr(3 + 0.4*math.Sin(5*angle) + rng.NormFloat64()*0.1),
			Course:             ptr(math.Mod(angle*180/math.Pi+90+rng.NormFloat64()*3+360, 360)),
		}
	}

	return points
}
