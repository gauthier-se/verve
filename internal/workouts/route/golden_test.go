package route_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/workouts/route"
)

// update rewrites the golden files, whose diff then shows every change of the encoding.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// fixturePoint is a point of a fixture route in testdata.
type fixturePoint struct {
	Time               time.Time `json:"time"`
	Lat                float64   `json:"lat"`
	Lon                float64   `json:"lon"`
	Altitude           *float64  `json:"altitude"`
	HorizontalAccuracy *float64  `json:"horizontalAccuracy"`
	VerticalAccuracy   *float64  `json:"verticalAccuracy"`
	Speed              *float64  `json:"speed"`
	Course             *float64  `json:"course"`
}

// readFixture returns the points of the fixture route at path.
func readFixture(t *testing.T, path string) []route.Point {
	t.Helper()

	content, err := os.ReadFile(path) //nolint:gosec // G304: a fixture of testdata
	require.NoError(t, err)

	var fixture []fixturePoint
	require.NoError(t, json.Unmarshal(content, &fixture))

	points := make([]route.Point, len(fixture))
	for i, p := range fixture {
		points[i] = route.Point(p)
	}

	return points
}

// requireGolden requires that got equals the golden file at path, or rewrites it with -update.
func requireGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}

	want, err := os.ReadFile(path) //nolint:gosec // G304: a golden file of testdata
	require.NoError(t, err)
	require.Equal(t, want, got, "%s differs, run the tests with -update to rewrite it", path)
}

// Given the fixture routes in testdata, then their encodings and simplified polylines equal their
// golden files, and -update rewrites them.
func TestCodec_FixtureRoutes_MatchGoldenFiles(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	codec := newCodec(t)

	for _, fixture := range fixtures {
		name := strings.TrimSuffix(fixture, ".json")

		t.Run(filepath.Base(name), func(t *testing.T) {
			t.Parallel()

			points := readFixture(t, fixture)

			data, err := codec.Encode(points)
			require.NoError(t, err)
			requireGolden(t, name+".route", data)

			simplified, _ := route.Simplify(points)
			requireGolden(t, name+".polyline", []byte(route.Polyline(simplified)+"\n"))

			decoded, err := codec.Decode(data)
			require.NoError(t, err)
			require.Len(t, decoded, len(points))
		})
	}
}

// Given a route of one point per second, then its encoding is at most a quarter of the size of its
// fields as float64 columns.
func TestCodec_OnePointPerSecond_QuarterOfFloat64Columns(t *testing.T) {
	t.Parallel()

	const fields = 8

	points := windingRoute(3600)

	data, err := newCodec(t).Encode(points)
	require.NoError(t, err)

	columns := len(points) * fields * 8
	t.Logf("%d points: %d bytes, %d as float64 columns", len(points), len(data), columns)
	require.LessOrEqual(t, len(data), columns/4)
}
