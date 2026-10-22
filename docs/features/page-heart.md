# Heart page

- **Status:** Planned
- **Milestone:** M4
- **Related:** [Metric explorer](page-metric-explorer.md), [Workouts](page-workouts.md), [User preferences and profile](user-preferences-and-profile.md)

## Summary

A dedicated page for cardiovascular metrics: resting heart rate, heart rate variability with a personal baseline, daily heart rate range, walking heart rate, cardio fitness (VO2max), heart rate recovery, time in zones, heart rate notifications and vitals such as blood pressure and oxygen saturation.

## User stories

- As a user, I want to see whether my resting heart rate and HRV are within my usual range.
- As a user, I want to follow my cardio fitness over months.
- As a user, I want to review high, low or irregular heart rate notifications.

## Scope

### In scope

- Curated heart and vitals views built on stored samples and aggregates.

### Out of scope

- Diagnosis, risk scores or medical interpretation.
- ECG recordings (future).

## Functional requirements

- **FR-1**: Resting heart rate trend with a 7-day average line.
- **FR-2**: Heart rate variability (SDNN, as recorded by Apple Watch) trend with a personal baseline band: 30-day mean plus or minus one standard deviation, values outside the band highlighted neutrally.
- **FR-3**: Daily heart rate range chart (min to max band with daily mean).
- **FR-4**: Walking heart rate average trend.
- **FR-5**: Cardio fitness (VO2max) trend over long periods, without classification labels.
- **FR-6**: Heart rate recovery (one minute after workouts) trend when available.
- **FR-7**: Weekly time in heart rate zones across all workouts.
- **FR-8**: Heart rate notifications (high, low, irregular rhythm) listed with date and context, with a link to the heart rate of that day.
- **FR-9**: Vitals section when data exists: blood pressure (systolic and diastolic over time), oxygen saturation, respiratory rate.
- **FR-10**: Period selector shared by all sections, stored in the URL.

## UX notes

- Sections are collapsible and hidden when no data exists.
- Neutral wording: "outside your usual range", never alarming language.

## Data and API notes

- Reads aggregates and samples through existing endpoints, plus `GET /api/v1/heart/summary?from&to` for baseline computations.
- Chart components: `TimeSeriesChart`, `RangeBandChart`, `ZoneBars`.

## Edge cases

- HRV measured at irregular times (Apple Watch samples a few times a day): daily mean used for trends.
- Beta blockers or illness can shift baselines: the baseline window is configurable (14, 30 or 60 days).

## Acceptance criteria

- [ ] Each section appears only when its data exists.
- [ ] The HRV baseline band is computed from the configured window.

## Future ideas

- ECG recordings viewer.
- Correlation of HRV with sleep and training load.
