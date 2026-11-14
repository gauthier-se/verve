package aggregate

import "time"

// hourStart returns the start of the local hour of loc that holds t, as a UTC instant (FR-6 of
// Storage and aggregates, ADR 0012): t without the minutes and seconds of its local time, or the
// start of the zone of t when the offset changed since, so that the buckets partition time even
// where the offset changes by less than an hour.
func hourStart(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	start := t.Add(-sinceLocalHour(local))

	if zoneStart, _ := local.ZoneBounds(); !zoneStart.IsZero() && zoneStart.After(start) {
		start = zoneStart
	}

	return start.UTC()
}

// hourEnd returns the end of the local hour of loc that starts at start: the next local hour, or
// the end of the zone of start when the offset changes before.
func hourEnd(start time.Time, loc *time.Location) time.Time {
	local := start.In(loc)
	end := start.Add(time.Hour - sinceLocalHour(local))

	if _, zoneEnd := local.ZoneBounds(); !zoneEnd.IsZero() && zoneEnd.Before(end) {
		end = zoneEnd
	}

	return end.UTC()
}

// sinceLocalHour returns the time elapsed since the local hour of local started on the clock.
func sinceLocalHour(local time.Time) time.Duration {
	return time.Duration(local.Minute())*time.Minute + time.Duration(local.Second())*time.Second +
		time.Duration(local.Nanosecond())
}

// localHours returns the starts of the local hours of loc that overlap [from, to), oldest first,
// or the hour that holds from when the range is empty.
func localHours(from, to time.Time, loc *time.Location) []time.Time {
	hours := []time.Time{hourStart(from, loc)}

	for {
		next := hourEnd(hours[len(hours)-1], loc)
		if !next.Before(to) {
			return hours
		}

		hours = append(hours, next)
	}
}

// dayStart returns the local midnight of loc that starts the local day of t, as a UTC instant.
func dayStart(t time.Time, loc *time.Location) time.Time {
	year, month, day := t.In(loc).Date()

	return time.Date(year, month, day, 0, 0, 0, 0, loc).UTC()
}

// dayEnd returns the start of the local day of loc after the one that starts at start.
func dayEnd(start time.Time, loc *time.Location) time.Time {
	year, month, day := start.In(loc).Date()

	return time.Date(year, month, day+1, 0, 0, 0, 0, loc).UTC()
}

// monthStart returns the local midnight of loc that starts the local month of t, as a UTC instant.
func monthStart(t time.Time, loc *time.Location) time.Time {
	year, month, _ := t.In(loc).Date()

	return time.Date(year, month, 1, 0, 0, 0, 0, loc).UTC()
}

// monthEnd returns the start of the local month of loc after the one that starts at start.
func monthEnd(start time.Time, loc *time.Location) time.Time {
	year, month, _ := start.In(loc).Date()

	return time.Date(year, month+1, 1, 0, 0, 0, 0, loc).UTC()
}

// overlap returns the overlap of the sample [start, end) with the bucket [from, to), and whether
// the sample overlaps the bucket. An instantaneous sample overlaps the bucket that holds its
// instant, by nothing.
func overlap(start, end, from, to time.Time) (time.Duration, bool) {
	if !end.After(start) {
		return 0, !start.Before(from) && start.Before(to)
	}

	if !start.Before(to) || !end.After(from) {
		return 0, false
	}

	if to.Before(end) {
		end = to
	}

	if from.After(start) {
		start = from
	}

	return end.Sub(start), true
}

// prorate returns the share of value, the value of the sample [start, end), that the bucket
// [from, to) holds, by overlap duration (FR-8 of Storage and aggregates), and whether the sample
// overlaps the bucket. An instantaneous sample gives its whole value to the bucket of its instant.
func prorate(value float64, start, end, from, to time.Time) (float64, bool) {
	shared, ok := overlap(start, end, from, to)

	switch {
	case !ok:
		return 0, false
	case !end.After(start):
		return value, true
	default:
		return value * float64(shared) / float64(end.Sub(start)), true
	}
}
