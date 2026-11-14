package aggregate

import "math"

// Downsample returns at most maxPoints of points, sorted by x, by largest triangle three buckets
// (FR-12 of Storage and aggregates), which keeps the shape of a line: the first and last points,
// and of each bucket of the points between them, the point that makes the largest triangle with the
// point kept before it and the mean of the next bucket. coordinates returns the x and y of a point.
// It returns points when they are at most maxPoints, and the first and last when maxPoints is below
// 3.
func Downsample[T any](points []T, maxPoints int, coordinates func(T) (float64, float64)) []T {
	if len(points) <= maxPoints {
		return points
	}

	last := len(points) - 1

	if maxPoints < 3 {
		return []T{points[0], points[last]}
	}

	kept := make([]T, 0, maxPoints)
	kept = append(kept, points[0])

	// The points between the first and the last fill maxPoints - 2 buckets of (len - 2) / (maxPoints -
	// 2) points each, at least one, bounded in integers so that the last bucket ends at the last point.
	bucketStart := func(i int) int { return i*(len(points)-2)/(maxPoints-2) + 1 }

	ax, ay := coordinates(points[0])

	for i := range maxPoints - 2 {
		// The mean of the next bucket, or the last point after the last bucket.
		nextFrom, nextTo := bucketStart(i+1), min(bucketStart(i+2), len(points))

		var meanX, meanY float64

		for _, p := range points[nextFrom:nextTo] {
			x, y := coordinates(p)
			meanX += x
			meanY += y
		}

		meanX /= float64(nextTo - nextFrom)
		meanY /= float64(nextTo - nextFrom)

		chosen, largest := bucketStart(i), -1.0

		for j := bucketStart(i); j < bucketStart(i+1); j++ {
			x, y := coordinates(points[j])

			// Twice the area: the comparison does not need the half.
			area := math.Abs((ax-meanX)*(y-ay) - (ax-x)*(meanY-ay))
			if area > largest {
				chosen, largest = j, area
			}
		}

		kept = append(kept, points[chosen])
		ax, ay = coordinates(points[chosen])
	}

	return append(kept, points[last])
}
