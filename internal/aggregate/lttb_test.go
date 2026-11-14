package aggregate

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// update rewrites the golden files, whose diff then shows every change of the downsampling.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// xy is a point of the downsampling tests.
type xy struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// coordinates returns the coordinates of p.
func coordinates(p xy) (float64, float64) {
	return p.X, p.Y
}

// line returns the points of ys at x = 0, 1, 2 and so on.
func line(ys ...float64) []xy {
	points := make([]xy, len(ys))
	for i, y := range ys {
		points[i] = xy{X: float64(i), Y: y}
	}

	return points
}

// Given seven points downsampled to four, then the first and last are kept, and of each of the two
// buckets between them, the point of the largest triangle with the point kept before it and the mean
// of the next bucket, worked out by hand.
func TestDownsample_KeepsThePointOfTheLargestTriangleOfEachBucket(t *testing.T) {
	t.Parallel()

	// Buckets of 2.5 points: {1, 2} and {3, 4, 5}. In the first, with (0, 0) and the mean (4, 11/6)
	// of the second, (1, 1) makes a triangle of area 13/12 and (2, 5) one of 49/6. In the second, with
	// (2, 5) and the last point (6, 0), (3, 1) makes 11/2, (4, 0) makes 5 and (5, 4.5) makes 13/2.
	points := line(0, 1, 5, 1, 0, 4.5, 0)

	require.Equal(t, []xy{{0, 0}, {2, 5}, {5, 4.5}, {6, 0}}, Downsample(points, 4, coordinates))
}

// Given at most as many points as asked, then every point is kept. Given two points asked, then the
// first and the last. Given no point, then none.
func TestDownsample_KeepsTheFirstAndTheLastPoints(t *testing.T) {
	t.Parallel()

	points := line(3, 1, 4, 1, 5)

	require.Equal(t, points, Downsample(points, 5, coordinates))
	require.Equal(t, points, Downsample(points, 5000, coordinates))
	require.Equal(t, []xy{{0, 3}, {4, 5}}, Downsample(points, 2, coordinates))
	require.Equal(t, []xy{{0, 3}}, Downsample(line(3), 2, coordinates))
	require.Empty(t, Downsample([]xy{}, 3, coordinates))
}

// Given points at uneven times, then the triangles are measured on their times, not their positions.
func TestDownsample_MeasuresTrianglesOnTheCoordinates(t *testing.T) {
	t.Parallel()

	// One bucket {1, 2} between (0, 0) and (10, 10): (1, 0) makes a triangle of area 5 and (9, 7) one
	// of 10. On their positions, (9, 7) at x = 2 would make one of 1/2, and (1, 0) would win.
	points := []xy{{0, 0}, {1, 0}, {9, 7}, {10, 10}}

	require.Equal(t, []xy{{0, 0}, {9, 7}, {10, 10}}, Downsample(points, 3, coordinates))
}

// Given the golden inputs in testdata, then their downsampling equals their golden files, and
// -update rewrites them.
func TestDownsample_GoldenInputs(t *testing.T) {
	t.Parallel()

	inputs, err := filepath.Glob(filepath.Join("testdata", "lttb", "*.input.json"))
	require.NoError(t, err)
	require.NotEmpty(t, inputs)

	for _, input := range inputs {
		name := input[:len(input)-len(".input.json")]

		t.Run(filepath.Base(name), func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(input) //nolint:gosec // G304: an input of testdata
			require.NoError(t, err)

			var golden struct {
				MaxPoints int  `json:"maxPoints"`
				Points    []xy `json:"points"`
			}
			require.NoError(t, json.Unmarshal(content, &golden))

			got := Downsample(golden.Points, golden.MaxPoints, coordinates)
			require.LessOrEqual(t, len(got), golden.MaxPoints)
			require.Equal(t, golden.Points[0], got[0])
			require.Equal(t, golden.Points[len(golden.Points)-1], got[len(got)-1])

			raw, err := json.MarshalIndent(got, "", "  ")
			require.NoError(t, err)

			path := name + ".golden.json"
			if *update {
				require.NoError(t, os.WriteFile(path, append(raw, '\n'), 0o600))
			}

			want, err := os.ReadFile(path) //nolint:gosec // G304: a golden file of testdata
			require.NoError(t, err)
			require.JSONEq(t, string(want), string(raw), "%s differs, run the tests with -update to rewrite it", path)
		})
	}
}
