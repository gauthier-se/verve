package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mustLoad returns the location of the IANA timezone name.
func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	require.NoError(t, err)

	return loc
}

// utc returns the UTC instant of the RFC 3339 time value.
func utc(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)

	return parsed.UTC()
}

// Given the timezones Asia/Kolkata (UTC+5:30) and Asia/Kathmandu (UTC+5:45), then the hourly bucket
// of an instant starts on its local hour and lasts one hour. Given UTC, as for a user without a
// timezone, then the buckets are the UTC hours.
func TestHourBucket_StartsOnLocalHours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		zone  string
		at    string
		start string
		end   string
	}{
		{zone: "Asia/Kolkata", at: "2026-11-14T10:10:00Z", start: "2026-11-14T09:30:00Z", end: "2026-11-14T10:30:00Z"},
		{zone: "Asia/Kolkata", at: "2026-11-14T09:30:00Z", start: "2026-11-14T09:30:00Z", end: "2026-11-14T10:30:00Z"},
		{zone: "Asia/Kathmandu", at: "2026-11-14T10:10:00Z", start: "2026-11-14T09:15:00Z", end: "2026-11-14T10:15:00Z"},
		{zone: "Asia/Kathmandu", at: "2026-11-14T09:14:59Z", start: "2026-11-14T08:15:00Z", end: "2026-11-14T09:15:00Z"},
		{zone: "UTC", at: "2026-11-14T10:59:59.999Z", start: "2026-11-14T10:00:00Z", end: "2026-11-14T11:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.zone+" "+tt.at, func(t *testing.T) {
			t.Parallel()

			loc := mustLoad(t, tt.zone)

			start := hourStart(utc(t, tt.at), loc)
			require.Equal(t, utc(t, tt.start), start)
			require.Equal(t, time.UTC, start.Location())
			require.Equal(t, utc(t, tt.end), hourEnd(start, loc))
		})
	}
}

// Given the days when daylight saving time starts and ends in Europe/Paris, then they hold 23 and
// 25 hourly buckets, from local midnight to local midnight, and the repeated local hour of autumn
// is two buckets.
func TestDayBucket_HoldsTheHoursOfTheLocalDay(t *testing.T) {
	t.Parallel()

	paris := mustLoad(t, "Europe/Paris")

	tests := []struct {
		name  string
		at    string
		start string
		end   string
		hours int
	}{
		{name: "spring", at: "2026-03-29T12:00:00Z", start: "2026-03-28T23:00:00Z", end: "2026-03-29T22:00:00Z", hours: 23},
		{name: "autumn", at: "2026-10-25T12:00:00Z", start: "2026-10-24T22:00:00Z", end: "2026-10-25T23:00:00Z", hours: 25},
		{name: "ordinary", at: "2026-11-14T23:30:00Z", start: "2026-11-14T23:00:00Z", end: "2026-11-15T23:00:00Z", hours: 24},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start := dayStart(utc(t, tt.at), paris)
			require.Equal(t, utc(t, tt.start), start)

			end := dayEnd(start, paris)
			require.Equal(t, utc(t, tt.end), end)

			hours := localHours(start, end, paris)
			require.Len(t, hours, tt.hours)
			require.Equal(t, start, hours[0])

			for i, hour := range hours {
				require.Equal(t, hour.Add(time.Hour), hourEnd(hour, paris), "hour %d", i)
			}
		})
	}

	autumn := localHours(utc(t, "2026-10-25T00:00:00Z"), utc(t, "2026-10-25T02:00:00Z"), paris)
	require.Equal(t, []time.Time{utc(t, "2026-10-25T00:00:00Z"), utc(t, "2026-10-25T01:00:00Z")}, autumn)
	require.Equal(t, "02:00 CEST", autumn[0].In(paris).Format("15:04 MST"))
	require.Equal(t, "02:00 CET", autumn[1].In(paris).Format("15:04 MST"))
}

// Given a range that starts and ends within local hours, then localHours returns every local hour
// that overlaps it, and an empty range the hour that holds its instant.
func TestLocalHours_ReturnsTheHoursThatOverlapARange(t *testing.T) {
	t.Parallel()

	kolkata := mustLoad(t, "Asia/Kolkata")

	require.Equal(t, []time.Time{
		utc(t, "2026-11-14T08:30:00Z"), utc(t, "2026-11-14T09:30:00Z"), utc(t, "2026-11-14T10:30:00Z"),
	}, localHours(utc(t, "2026-11-14T09:00:00Z"), utc(t, "2026-11-14T10:31:00Z"), kolkata))

	require.Equal(t, []time.Time{utc(t, "2026-11-14T09:30:00Z")},
		localHours(utc(t, "2026-11-14T09:30:00Z"), utc(t, "2026-11-14T10:30:00Z"), kolkata))

	require.Equal(t, []time.Time{utc(t, "2026-11-14T09:30:00Z")},
		localHours(utc(t, "2026-11-14T10:00:00Z"), utc(t, "2026-11-14T10:00:00Z"), kolkata))
}

// Given the local months of Europe/Paris, then a month starts at the local midnight of its first
// day, and ends at the start of the next one.
func TestMonthBucket_StartsAtLocalMidnight(t *testing.T) {
	t.Parallel()

	paris := mustLoad(t, "Europe/Paris")

	start := monthStart(utc(t, "2026-10-31T23:30:00Z"), paris)
	require.Equal(t, utc(t, "2026-10-31T23:00:00Z"), start)
	require.Equal(t, utc(t, "2026-11-30T23:00:00Z"), monthEnd(start, paris))

	start = monthStart(utc(t, "2026-10-15T00:00:00Z"), paris)
	require.Equal(t, utc(t, "2026-09-30T22:00:00Z"), start)
	require.Equal(t, utc(t, "2026-10-31T23:00:00Z"), monthEnd(start, paris))
}

// Given a cumulative sample of 120 spanning three hours, then each hour gets the share of the value
// of its overlap with the sample, and an hour it does not overlap nothing. Given an instantaneous
// sample, then the hour that holds its instant gets the whole value, and the others nothing.
func TestProrate_SharesAValueByOverlap(t *testing.T) {
	t.Parallel()

	start, end := utc(t, "2026-11-14T10:30:00Z"), utc(t, "2026-11-14T12:30:00Z")

	tests := []struct {
		hour  string
		value float64
		ok    bool
	}{
		{hour: "2026-11-14T09:00:00Z", value: 0, ok: false},
		{hour: "2026-11-14T10:00:00Z", value: 30, ok: true},
		{hour: "2026-11-14T11:00:00Z", value: 60, ok: true},
		{hour: "2026-11-14T12:00:00Z", value: 30, ok: true},
		{hour: "2026-11-14T12:30:00Z", value: 0, ok: false},
	}

	for _, tt := range tests {
		hour := utc(t, tt.hour)

		value, ok := prorate(120, start, end, hour, hour.Add(time.Hour))
		require.Equal(t, tt.ok, ok, tt.hour)
		require.InDelta(t, tt.value, value, 1e-9, tt.hour)
	}

	instant := utc(t, "2026-11-14T11:00:00Z")

	value, ok := prorate(7, instant, instant, instant, instant.Add(time.Hour))
	require.True(t, ok)
	require.InDelta(t, 7, value, 0)

	_, ok = prorate(7, instant, instant, instant.Add(-time.Hour), instant)
	require.False(t, ok)
}
