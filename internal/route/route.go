// Package route turns a stored GPX artifact into something a screen can draw: a
// simplified polyline and the profiles that hang off it. It is the only code in
// Verve that understands GPX.
//
// A Route is served as its own resource rather than as a Series (ADR 0028). The
// day-resolution cap on the aggregated-bucket contract (ADR 0012) is about an
// Account's measurements folded into buckets; this is one stored file rendered
// as itself, so nothing here reads a measurement and the cap is untouched.
//
// Parsing happens on demand and nothing is cached: a detail view is opened
// rarely, the parse costs tens of milliseconds, and a derived file sitting
// beside a content-addressed artifact would bring an invalidation question and
// an asterisk on "backup is copying a folder" (ADR 0004).
package route

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"time"
)

// earthRadiusKm is the mean radius used for haversine distances. A GPS trace is
// accurate to metres at best, so a spherical earth is far below the noise floor.
const earthRadiusKm = 6371.0088

// Point is one recorded position. Elevation and Time are optional: a GPX may
// omit either, and a point missing one is still a point on the map.
type Point struct {
	Lat float64
	Lon float64
	Ele *float64 // metres
	At  *time.Time
}

// Track is a parsed GPX: its points in recorded order.
type Track struct {
	Points []Point
}

// gpx mirrors only what Verve reads. Everything else in the file, including
// Apple's extensions (speed, course, accuracy), is ignored rather than modelled:
// the parser's job is a polyline and two profiles.
type gpx struct {
	Tracks []struct {
		Segments []struct {
			Points []struct {
				Lat  float64  `xml:"lat,attr"`
				Lon  float64  `xml:"lon,attr"`
				Ele  *float64 `xml:"ele"`
				Time string   `xml:"time"`
			} `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
}

// Parse reads a GPX into a Track. Apple writes one <trkseg>; several are
// tolerated and concatenated, because they are segments of one recording rather
// than separate Routes (separate Routes arrive as separate files, and those are
// never joined, ADR 0028).
//
// A malformed file is an error. A point missing its elevation or its timestamp
// is not: it keeps its place in the line and drops out of the profile that
// needed the missing field.
func Parse(r io.Reader) (Track, error) {
	var doc gpx
	dec := xml.NewDecoder(r)
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return Track{}, fmt.Errorf("route: parse gpx: %w", err)
	}

	var track Track
	for _, trk := range doc.Tracks {
		for _, seg := range trk.Segments {
			for _, p := range seg.Points {
				pt := Point{Lat: p.Lat, Lon: p.Lon, Ele: p.Ele}
				if p.Time != "" {
					if t, err := time.Parse(time.RFC3339, p.Time); err == nil {
						utc := t.UTC()
						pt.At = &utc
					}
				}
				track.Points = append(track.Points, pt)
			}
		}
	}
	return track, nil
}

// Simplify reduces a Track to at most maxPoints using Douglas-Peucker, keeping
// the shape and dropping the redundancy. A ride records a point per second, so a
// two-hour outing is some 7000 points of which a drawn line needs a fraction;
// shipping them all would send megabytes to draw one curve.
//
// The tolerance is searched rather than fixed: the right value depends on how
// far the ride went, and a constant that suits a 5 km run erases a 200 km ride's
// corners. Both endpoints are always kept.
func Simplify(t Track, maxPoints int) Track {
	if maxPoints < 2 || len(t.Points) <= maxPoints {
		return t
	}

	// Douglas-Peucker is n log n on a well-behaved trace and quadratic on a
	// pathological one (a zigzag where every point is a corner). A stride pass down
	// to a generous multiple of the target bounds the work whatever the shape, and
	// at eight times the points a drawn line will keep, it cannot be seen.
	points := t.Points
	if over := maxPoints * 8; len(points) > over {
		stride := (len(points) + over - 1) / over
		thinned := make([]Point, 0, over+1)
		for i, p := range points {
			if i%stride == 0 || i == len(points)-1 {
				thinned = append(thinned, p)
			}
		}
		points = thinned
	}

	// Bracket a tolerance (in km) that brings the count under the cap, then
	// bisect for a value that keeps as much detail as the cap allows.
	lo, hi := 0.0, 1.0
	for len(douglasPeucker(points, hi)) > maxPoints && hi < 1000 {
		hi *= 4
	}
	best := douglasPeucker(points, hi)
	for i := 0; i < 12; i++ {
		mid := (lo + hi) / 2
		got := douglasPeucker(points, mid)
		if len(got) > maxPoints {
			lo = mid
			continue
		}
		best = got
		hi = mid
	}
	return Track{Points: best}
}

// douglasPeucker keeps the points whose distance from the chord exceeds
// tolerance, recursively. Iterative to keep a long trace off the call stack.
func douglasPeucker(points []Point, tolerance float64) []Point {
	if len(points) < 3 {
		return points
	}
	keep := make([]bool, len(points))
	keep[0], keep[len(points)-1] = true, true

	type span struct{ first, last int }
	stack := []span{{0, len(points) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s.last <= s.first+1 {
			continue
		}
		maxDist, maxIdx := 0.0, s.first
		for i := s.first + 1; i < s.last; i++ {
			if d := perpendicularKm(points[i], points[s.first], points[s.last]); d > maxDist {
				maxDist, maxIdx = d, i
			}
		}
		if maxDist <= tolerance {
			continue
		}
		keep[maxIdx] = true
		stack = append(stack, span{s.first, maxIdx}, span{maxIdx, s.last})
	}

	out := make([]Point, 0, len(points))
	for i, k := range keep {
		if k {
			out = append(out, points[i])
		}
	}
	return out
}

// perpendicularKm is p's distance from the segment (a, b), in km. Latitude and
// longitude are projected flat with a cosine correction for the local meridian
// convergence, which is exact enough over the span of one workout.
func perpendicularKm(p, a, b Point) float64 {
	cos := math.Cos(a.Lat * math.Pi / 180)
	const degKm = 111.32
	px, py := (p.Lon-a.Lon)*cos*degKm, (p.Lat-a.Lat)*degKm
	bx, by := (b.Lon-a.Lon)*cos*degKm, (b.Lat-a.Lat)*degKm

	den := bx*bx + by*by
	if den == 0 {
		return math.Hypot(px, py)
	}
	t := (px*bx + py*by) / den
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-t*bx, py-t*by)
}

// distanceKm is the great-circle distance between two points.
func distanceKm(a, b Point) float64 {
	lat1, lat2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dLat, dLon := lat2-lat1, (b.Lon-a.Lon)*math.Pi/180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Sample is one point of a profile: a distance along the trace, in km, with what
// was true there. Ele and Speed are nil where the recording did not support them.
type Sample struct {
	Km    float64  `json:"km"`
	Ele   *float64 `json:"ele,omitempty"`   // metres
	Speed *float64 `json:"speed,omitempty"` // km/h; the client renders pace from it
}

// Profiles is a Route's shape against distance travelled, plus the figures that
// describe it. LengthKm is the geometry's own length and belongs to this axis
// only: what a screen reports as the workout's distance is the Session's
// total_distance, which is what the device measured (ADR 0028).
type Profiles struct {
	Samples  []Sample `json:"samples"`
	LengthKm float64  `json:"length_km"`
	AscentM  float64  `json:"ascent_m"`
	DescentM float64  `json:"descent_m"`
	MinEleM  *float64 `json:"min_ele_m,omitempty"`
	MaxEleM  *float64 `json:"max_ele_m,omitempty"`
}

// ascentThresholdM is the elevation change a climb must exceed before it counts.
// Consumer GPS altitude wanders by a few metres while standing still, and summing
// every positive delta turns that wander into hundreds of metres of ascent that
// nobody climbed.
const ascentThresholdM = 3.0

// speedWindow is how many points the speed is averaged over. Per-point speed
// from consumer GPS is noise: a one-second gap divides a metre of jitter by a
// second and reports a sprint.
const speedWindow = 5

// Compute derives the profiles from a Track, in recorded order, emitting at most
// maxSamples of them.
//
// The figures are measured over every recorded point and the decimation applies
// only to what is emitted. Measuring a simplified track instead would lose the
// climbs the simplification flattened: Douglas-Peucker works on the ground plan,
// so a hill climbed on a straight road collapses to its two endpoints and its
// ascent disappears.
func Compute(t Track, maxSamples int) Profiles {
	p := Profiles{Samples: []Sample{}}
	if len(t.Points) == 0 {
		return p
	}

	cum := make([]float64, len(t.Points))
	for i := 1; i < len(t.Points); i++ {
		cum[i] = cum[i-1] + distanceKm(t.Points[i-1], t.Points[i])
	}
	p.LengthKm = cum[len(cum)-1]

	// Ascent and descent, hysteresis-filtered: a run of drift is only counted
	// once it has moved further than the noise it sits in.
	var anchor *float64
	for _, pt := range t.Points {
		if pt.Ele == nil {
			continue
		}
		ele := *pt.Ele
		if p.MinEleM == nil || ele < *p.MinEleM {
			v := ele
			p.MinEleM = &v
		}
		if p.MaxEleM == nil || ele > *p.MaxEleM {
			v := ele
			p.MaxEleM = &v
		}
		if anchor == nil {
			v := ele
			anchor = &v
			continue
		}
		switch d := ele - *anchor; {
		case d > ascentThresholdM:
			p.AscentM += d
			*anchor = ele
		case d < -ascentThresholdM:
			p.DescentM += -d
			*anchor = ele
		}
	}

	stride := 1
	if maxSamples > 1 && len(t.Points) > maxSamples {
		stride = (len(t.Points) + maxSamples - 1) / maxSamples
	}
	for i, pt := range t.Points {
		if i%stride != 0 && i != len(t.Points)-1 {
			continue
		}
		s := Sample{Km: cum[i], Ele: pt.Ele}
		if v, ok := speedAt(t.Points, cum, i); ok {
			s.Speed = &v
		}
		p.Samples = append(p.Samples, s)
	}
	return p
}

// speedAt is the average speed around point i, in km/h, over a window of
// recorded points. It reports nothing where the window has no timestamps or no
// elapsed time, rather than dividing by zero and calling the result a speed.
func speedAt(points []Point, cum []float64, i int) (float64, bool) {
	lo, hi := i-speedWindow/2, i+speedWindow/2
	if lo < 0 {
		lo = 0
	}
	if hi > len(points)-1 {
		hi = len(points) - 1
	}
	if points[lo].At == nil || points[hi].At == nil {
		return 0, false
	}
	secs := points[hi].At.Sub(*points[lo].At).Seconds()
	if secs <= 0 {
		return 0, false
	}
	return (cum[hi] - cum[lo]) / (secs / 3600), true
}
