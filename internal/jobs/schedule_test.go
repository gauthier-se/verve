package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// parisLocation returns the time zone of the daylight saving tests. In 2026, its clocks go forward
// on March 29 at 02:00, and back on October 25 at 03:00.
func parisLocation(t *testing.T) *time.Location {
	t.Helper()

	location, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)

	return location
}

// Given a schedule, then its next run follows its interval, or its cron expression read in the
// location of now, around a daylight saving change too.
func TestSchedule_Next(t *testing.T) {
	t.Parallel()

	paris := parisLocation(t)
	daily := Schedule{Cron: "0 3 * * *"}
	inTheChangedHour := Schedule{Cron: "30 2 * * *"}

	tests := []struct {
		name     string
		schedule Schedule
		now      time.Time
		want     time.Time
	}{
		{
			name:     "IntervalIsCountedFromNow",
			schedule: Schedule{Every: 90 * time.Minute},
			now:      time.Date(2026, 7, 1, 10, 0, 0, 0, paris),
			want:     time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC),
		},
		{
			name:     "IntervalAcrossAChangeIsRealTime",
			schedule: Schedule{Every: 24 * time.Hour},
			now:      time.Date(2026, 3, 28, 12, 0, 0, 0, paris),
			want:     time.Date(2026, 3, 29, 11, 0, 0, 0, time.UTC),
		},
		{
			name:     "CronIsReadInTheLocationOfNow",
			schedule: daily,
			now:      time.Date(2026, 7, 1, 10, 0, 0, 0, paris),
			want:     time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC),
		},
		{
			name:     "CronMovesPastARunThatIsNow",
			schedule: daily,
			now:      time.Date(2026, 7, 2, 3, 0, 0, 0, paris),
			want:     time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC),
		},
		{
			name:     "CronAcrossTheChangeInSpring",
			schedule: daily,
			now:      time.Date(2026, 3, 28, 12, 0, 0, 0, paris),
			want:     time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC),
		},
		{
			name:     "CronAcrossTheChangeInAutumn",
			schedule: daily,
			now:      time.Date(2026, 10, 24, 12, 0, 0, 0, paris),
			want:     time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC),
		},
		{
			name:     "CronInTheHourSkippedInSpringWaitsForTheNextDay",
			schedule: inTheChangedHour,
			now:      time.Date(2026, 3, 28, 12, 0, 0, 0, paris),
			want:     time.Date(2026, 3, 30, 0, 30, 0, 0, time.UTC),
		},
		{
			name:     "CronInTheHourRepeatedInAutumnRunsTheFirstTime",
			schedule: inTheChangedHour,
			now:      time.Date(2026, 10, 25, 0, 0, 0, 0, paris),
			want:     time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC),
		},
		{
			name:     "CronInTheHourRepeatedInAutumnRunsTheSecondTime",
			schedule: inTheChangedHour,
			now:      time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC).In(paris),
			want:     time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.schedule.Next(tt.now)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.UTC())
		})
	}
}

// Given a cron expression that is invalid or that no date matches, then Next fails instead of
// returning a time that would always be due. A time zone in the expression is invalid too: cron
// expressions are read in the local time of the process, and the parser panics on some of them.
func TestSchedule_NextFailsOnACronExpressionThatCannotRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	for _, expression := range []string{
		"not a cron expression",
		"0 3 * *",
		"0 0 3 * * *",
		"99 3 * * *",
		"0 3 30 2 *",
		"TZ=UTC",
		"CRON_TZ=Europe/Paris 0 3 * * *",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()

			got, err := Schedule{Cron: expression}.Next(now)
			require.ErrorContains(t, err, "cron expression")
			require.ErrorContains(t, err, expression)
			require.Zero(t, got)
		})
	}
}
