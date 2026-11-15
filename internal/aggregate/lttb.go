package aggregate

import "math"

// Downsample returns at most maxPoints of points, sorted by x, by largest triangle three buckets
// (FR-12 of Storage and aggregates), which keeps the shape of a line: the first and last points,
// and of each bucket of the points between them, the point that makes the largest triangle with the
// point kept before it and the mean of the next bucket. coordinates returns the x and y of a point.
// It returns points when they are at most maxPoints, and the first and last when maxPoints is below
// 3.
func Downsample[T any](points []T, maxPoints int, coordinates func(T) (float64, float64)) []T {
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
// The points of a scan are positioned by the order of their visit, and the first and last points of
// the last scan are always kept: points added or removed between two scans change the points kept
// between them, never the first and last, the order or the bound of maxPoints.
func downsampleScan[T any](n, maxPoints int, scan func(visit func(T)) error,
	coordinates func(T) (float64, float64),
) ([]T, error) {
	switch {
	case n <= maxPoints:
		kept := make([]T, 0, n)
		err := scan(func(p T) {
			if len(kept) < maxPoints {
				kept = append(kept, p)
			} else {
				kept[len(kept)-1] = p
			}
		})

		return kept, err
	case maxPoints < 3:
		kept := make([]T, 0, 2)
		err := scan(func(p T) {
			if len(kept) < 2 {
				kept = append(kept, p)
			} else {
				kept[1] = p
			}
		})

		return kept, err
	}

	buckets := maxPoints - 2

	// The mean of each bucket, then of the last point, at index buckets, so that the next bucket of
	// bucket i is at i + 1. The order of the sums is that of the points.
	meanX, meanY, counts := make([]float64, buckets+1), make([]float64, buckets+1), make([]int, buckets+1)

	sums := newPositions(n, buckets)
	err := scan(func(p T) {
		if point, bucket := sums.next(); point > 0 && point < n {
			x, y := coordinates(p)
			meanX[bucket] += x
			meanY[bucket] += y
			counts[bucket]++
		}
	})
	if err != nil {
		return nil, err
	}

	for i := range meanX {
		meanX[i] /= float64(counts[i])
		meanY[i] /= float64(counts[i])
	}

	var (
		kept   = make([]T, 0, maxPoints)
		ax, ay float64
		// candidate is the point of the largest triangle of the bucket of the points visited last, at
		// position candidateAt, and pending is true until it is kept.
		candidate   T
		candidateAt int
		largest     float64
		pending     bool
		// last is the point visited last, at position lastAt.
		last          T
		lastAt        int
		currentBucket int
	)

	triangles := newPositions(n, buckets)
	err = scan(func(p T) {
		point, bucket := triangles.next()
		last, lastAt = p, point

		switch {
		case point == 0:
			kept = append(kept, p)
			ax, ay = coordinates(p)

			return
		case point >= n:
			return
		}

		if bucket != currentBucket {
			if pending {
				kept = append(kept, candidate)
				ax, ay = coordinates(candidate)
				pending = false
			}

			currentBucket = bucket
		}

		if bucket == buckets {
			kept = append(kept, p)

			return
		}

		if !pending {
			candidate, candidateAt, largest, pending = p, point, -1, true
		}

		x, y := coordinates(p)

		// Twice the area: the comparison does not need the half.
		area := math.Abs((ax-meanX[bucket+1])*(y-ay) - (ax-x)*(meanY[bucket+1]-ay))
		if area > largest {
			candidate, candidateAt, largest = p, point, area
		}
	})
	if err != nil {
		return nil, err
	}

	switch {
	// The last point, at n - 1, is kept already, and a point visited after it replaces it.
	case lastAt >= n-1:
		kept[len(kept)-1] = last
	// Fewer points than n: the bucket of the last point ends with it.
	case lastAt > 0:
		if pending && candidateAt != lastAt {
			kept = append(kept, candidate)
		}

		kept = append(kept, last)
	}

	return kept, nil
}

// positions gives the position of each point of a scan of n points, in the order of their visit,
// and its bucket among the buckets of the points between the first and the last: (n - 2) / buckets
// points each, at least one, bounded in integers so that the last bucket ends at the last point. The
// last point is in bucket buckets.
type positions struct {
	n, buckets int
	// point is the position of the next point, and bucket the bucket of the point before it.
	point, bucket int
}

func newPositions(n, buckets int) *positions {
	return &positions{n: n, buckets: buckets}
}

// next returns the position and the bucket of the next point of the scan. Its bucket is meaningless
// for the first point and those at n or beyond.
func (p *positions) next() (point, bucket int) {
	point = p.point
	p.point++

	for p.bucket < p.buckets && point >= p.start(p.bucket+1) {
		p.bucket++
	}

	return point, p.bucket
}

// start returns the position of the first point of bucket i, and of the last point for i = buckets.
func (p *positions) start(i int) int {
	return i*(p.n-2)/p.buckets + 1
}
