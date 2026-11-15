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

	kept, _ := downsampleScan(len(points), maxPoints, func(visit func(T)) error {
		for _, p := range points {
			visit(p)
		}

		return nil
	}, coordinates)

	return kept
}

// downsampleScan is Downsample of the n points that scan visits in order each time it is called, and
// holds maxPoints points and sums only, whatever n: it scans once to sum the coordinates of each
// bucket, and once more to keep the point of the largest triangle of each, or once when n is at most
// maxPoints or maxPoints is below 3. It returns the first error of scan.
//
// The points of a scan are positioned by the order of their visit: those beyond n are left out, and
// a scan of fewer points than n keeps the point chosen in the last bucket it reached, so that points
// added or removed between two scans change the points kept, but they stay sorted and at most
// maxPoints.
func downsampleScan[T any](n, maxPoints int, scan func(visit func(T)) error,
	coordinates func(T) (float64, float64),
) ([]T, error) {
	switch {
	case n <= maxPoints:
		kept := make([]T, 0, n)
		err := scan(func(p T) {
			if len(kept) < maxPoints {
				kept = append(kept, p)
			}
		})

		return kept, err
	case maxPoints < 3:
		kept, j := make([]T, 0, 2), 0
		err := scan(func(p T) {
			if j == 0 || j == n-1 {
				kept = append(kept, p)
			}

			j++
		})

		return kept, err
	}

	// The points between the first and the last fill maxPoints - 2 buckets of (n - 2) / (maxPoints -
	// 2) points each, at least one, bounded in integers so that the last bucket ends at the last point.
	buckets := maxPoints - 2
	bucketStart := func(i int) int { return i*(n-2)/buckets + 1 }

	// The mean of each bucket, then the last point, at index buckets, so that the next bucket of
	// bucket i is at i + 1. The order of the sums is that of the points.
	meanX, meanY, counts := make([]float64, buckets+1), make([]float64, buckets+1), make([]int, buckets+1)

	j, i := 0, 0
	err := scan(func(p T) {
		defer func() { j++ }()

		if j == 0 || j >= n {
			return
		}

		for i < buckets && j >= bucketStart(i+1) {
			i++
		}

		x, y := coordinates(p)
		meanX[i] += x
		meanY[i] += y
		counts[i]++
	})
	if err != nil {
		return nil, err
	}

	for k := range meanX {
		meanX[k] /= float64(counts[k])
		meanY[k] /= float64(counts[k])
	}

	var (
		kept    = make([]T, 0, maxPoints)
		ax, ay  float64
		chosen  T
		largest float64
		// open is true when bucket i has a chosen point not kept yet.
		open bool
	)

	j, i = 0, 0
	err = scan(func(p T) {
		defer func() { j++ }()

		switch {
		case j >= n:
			return
		case j == 0:
			kept = append(kept, p)
			ax, ay = coordinates(p)

			return
		}

		for i < buckets && j >= bucketStart(i+1) {
			if open {
				kept = append(kept, chosen)
				ax, ay = coordinates(chosen)
				open = false
			}

			i++
		}

		if i == buckets {
			kept = append(kept, p)

			return
		}

		if !open {
			chosen, largest, open = p, -1, true
		}

		x, y := coordinates(p)

		// Twice the area: the comparison does not need the half.
		area := math.Abs((ax-meanX[i+1])*(y-ay) - (ax-x)*(meanY[i+1]-ay))
		if area > largest {
			chosen, largest = p, area
		}
	})
	if err != nil {
		return nil, err
	}

	if open {
		kept = append(kept, chosen)
	}

	return kept, nil
}
