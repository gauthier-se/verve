package route

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// gpxOf builds a GPX document from trkpt fragments.
func gpxOf(points ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="Apple Health Export">
 <trk><name>Route</name><trkseg>
` + strings.Join(points, "\n") + `
 </trkseg></trk>
</gpx>`
}

func trkpt(lat, lon float64, ele string, at string) string {
	s := fmt.Sprintf(`  <trkpt lat="%f" lon="%f">`, lat, lon)
	if ele != "" {
		s += `<ele>` + ele + `</ele>`
	}
	if at != "" {
		s += `<time>` + at + `</time>`
	}
	return s + `</trkpt>`
}

func TestParseReadsPointsInOrder(t *testing.T) {
	doc := gpxOf(
		trkpt(48.8566, 2.3522, "35.0", "2024-05-01T06:00:00Z"),
		trkpt(48.8576, 2.3522, "40.0", "2024-05-01T06:00:30Z"),
	)
	track, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(track.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(track.Points))
	}
	if track.Points[0].Lat != 48.8566 || track.Points[1].Lon != 2.3522 {
		t.Errorf("coordinates not read in order: %+v", track.Points)
	}
	if track.Points[0].Ele == nil || *track.Points[0].Ele != 35 {
		t.Errorf("elevation = %v, want 35", track.Points[0].Ele)
	}
	if track.Points[0].At == nil || track.Points[0].At.Hour() != 6 {
		t.Errorf("time = %v, want 06:00Z", track.Points[0].At)
	}
}

// TestParseToleratesMissingFields: a point without elevation or time keeps its
// place on the map and drops out only of the profile that needed the field.
func TestParseToleratesMissingFields(t *testing.T) {
	doc := gpxOf(
		trkpt(48.8566, 2.3522, "", ""),
		trkpt(48.8576, 2.3532, "40.0", "2024-05-01T06:00:30Z"),
	)
	track, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(track.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(track.Points))
	}
	if track.Points[0].Ele != nil || track.Points[0].At != nil {
		t.Errorf("absent fields invented: %+v", track.Points[0])
	}
	p := Compute(track, 100)
	if p.Samples[0].Ele != nil {
		t.Error("a sample reported an elevation the file does not contain")
	}
}

// TestParseDegenerateInputs: every shape a stored artifact can take must return
// something renderable or a clean error, and never a panic.
func TestParseDegenerateInputs(t *testing.T) {
	cases := map[string]string{
		"empty document":   gpxOf(),
		"single point":     gpxOf(trkpt(48.8566, 2.3522, "35.0", "2024-05-01T06:00:00Z")),
		"no trkseg":        `<?xml version="1.0"?><gpx><trk><name>x</name></trk></gpx>`,
		"unparseable time": gpxOf(trkpt(48.8566, 2.3522, "35.0", "not a date")),
	}
	for name, doc := range cases {
		track, err := Parse(strings.NewReader(doc))
		if err != nil {
			t.Errorf("%s: Parse: %v", name, err)
			continue
		}
		p := Compute(track, 100)
		if p.Samples == nil {
			t.Errorf("%s: nil samples, want an empty slice a client can render", name)
		}
		if got := Simplify(track, 500); len(got.Points) > len(track.Points) {
			t.Errorf("%s: Simplify grew the track", name)
		}
	}

	if _, err := Parse(strings.NewReader("this is not xml at all")); err == nil {
		t.Error("a non-GPX file parsed without error")
	}
}

// TestLengthMatchesAKnownDistance: one degree of latitude is ~111.2 km.
func TestLengthMatchesAKnownDistance(t *testing.T) {
	doc := gpxOf(
		trkpt(48.0, 2.0, "", ""),
		trkpt(49.0, 2.0, "", ""),
	)
	track, _ := Parse(strings.NewReader(doc))
	p := Compute(track, 100)
	if math.Abs(p.LengthKm-111.2) > 0.5 {
		t.Errorf("LengthKm = %v, want ~111.2", p.LengthKm)
	}
}

// TestAscentIgnoresDrift is why ascent is hysteresis-filtered: consumer GPS
// altitude wanders by a few metres while standing still, and summing every
// positive delta reports a climb nobody made.
func TestAscentIgnoresDrift(t *testing.T) {
	var points []string
	for i := 0; i < 200; i++ {
		ele := 100.0
		if i%2 == 0 {
			ele = 101.5 // 1.5 m of jitter, 100 times over
		}
		points = append(points, trkpt(48.0+float64(i)*1e-5, 2.0, fmt.Sprintf("%.1f", ele), ""))
	}
	track, _ := Parse(strings.NewReader(gpxOf(points...)))
	if p := Compute(track, 500); p.AscentM != 0 {
		t.Errorf("AscentM = %v over pure jitter, want 0", p.AscentM)
	}

	// A real climb of 50 m is reported.
	points = nil
	for i := 0; i <= 50; i++ {
		points = append(points, trkpt(48.0+float64(i)*1e-4, 2.0, fmt.Sprintf("%d", 100+i), ""))
	}
	track, _ = Parse(strings.NewReader(gpxOf(points...)))
	if p := Compute(track, 500); p.AscentM < 45 || p.AscentM > 50 {
		t.Errorf("AscentM = %v over a 50 m climb, want ~50", p.AscentM)
	}
}

// TestSimplifyKeepsShapeAndEndpoints: the cap is respected, both ends survive,
// and a straight line collapses to its endpoints.
func TestSimplifyKeepsShapeAndEndpoints(t *testing.T) {
	var points []string
	for i := 0; i < 4000; i++ {
		// A zigzag, so the shape is not trivially removable.
		lon := 2.0 + float64(i%2)*1e-4
		points = append(points, trkpt(48.0+float64(i)*1e-5, lon, "", ""))
	}
	track, _ := Parse(strings.NewReader(gpxOf(points...)))

	got := Simplify(track, 500)
	if len(got.Points) > 500 {
		t.Errorf("simplified to %d points, want at most 500", len(got.Points))
	}
	if got.Points[0] != track.Points[0] {
		t.Error("the first point was dropped")
	}
	if got.Points[len(got.Points)-1] != track.Points[len(track.Points)-1] {
		t.Error("the last point was dropped")
	}

	straight := Track{Points: []Point{{Lat: 48, Lon: 2}, {Lat: 48.5, Lon: 2}, {Lat: 49, Lon: 2}}}
	if got := Simplify(straight, 2); len(got.Points) != 2 {
		t.Errorf("a straight line simplified to %d points, want 2", len(got.Points))
	}
}

// TestComputeDecimatesWithoutLosingTheClimb is the reason the profile measures
// the full track and decimates only its output: Douglas-Peucker works on the
// ground plan, so a hill climbed on a straight road collapses to two points and
// its ascent would vanish with it.
func TestComputeDecimatesWithoutLosingTheClimb(t *testing.T) {
	var points []string
	for i := 0; i <= 300; i++ {
		points = append(points, trkpt(48.0+float64(i)*1e-4, 2.0, fmt.Sprintf("%d", 100+i), ""))
	}
	track, _ := Parse(strings.NewReader(gpxOf(points...)))

	p := Compute(track, 50)
	if len(p.Samples) > 51 {
		t.Errorf("samples = %d, want at most 51", len(p.Samples))
	}
	if p.AscentM < 290 {
		t.Errorf("AscentM = %v after decimation, want ~300", p.AscentM)
	}
	if last := p.Samples[len(p.Samples)-1]; math.Abs(last.Km-p.LengthKm) > 1e-9 {
		t.Errorf("last sample at %v km, want the full length %v", last.Km, p.LengthKm)
	}
}

// TestSpeedNeedsTimestamps: no timestamps means no speed, rather than a division
// by zero dressed as a number.
func TestSpeedNeedsTimestamps(t *testing.T) {
	track, _ := Parse(strings.NewReader(gpxOf(
		trkpt(48.0, 2.0, "", ""),
		trkpt(48.001, 2.0, "", ""),
	)))
	for _, s := range Compute(track, 100).Samples {
		if s.Speed != nil {
			t.Errorf("speed %v reported without timestamps", *s.Speed)
		}
	}

	// 111.2 m in 40 s is ~10 km/h.
	timed, _ := Parse(strings.NewReader(gpxOf(
		trkpt(48.0, 2.0, "", "2024-05-01T06:00:00Z"),
		trkpt(48.001, 2.0, "", "2024-05-01T06:00:40Z"),
	)))
	samples := Compute(timed, 100).Samples
	if samples[0].Speed == nil {
		t.Fatal("no speed reported for a timed pair")
	}
	if got := *samples[0].Speed; got < 9 || got > 11 {
		t.Errorf("speed = %v km/h, want ~10", got)
	}
}
