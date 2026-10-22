# Sleep page

- **Status:** Planned
- **Milestone:** M2
- **Related:** [User preferences and profile](user-preferences-and-profile.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Health Auto Export endpoint](ingestion-health-auto-export.md)

## Summary

Nights are assembled from sleep analysis samples, attributed to the wake-up date using the user's sleep window. The page shows a hypnogram per night, nightly statistics and long-term trends for duration, stages and regularity.

## User stories

- As a user, I want to see the stages of last night like in the Health app, but on a bigger screen.
- As a user, I want to know if I sleep enough and at regular times.
- As a user with a third-party sleep app, I want to choose which source defines my nights.

## Scope

### In scope

- Night assembly, main sleep and naps.
- Night detail with hypnogram and overnight vitals.
- Trends: duration, stages, bedtime and wake time, regularity.

### Out of scope

- Proprietary or opaque sleep scores.
- Sleep apnea detection or any diagnosis.

## Functional requirements

- **FR-1**: Sleep samples (In Bed, Asleep unspecified, Core, Deep, REM, Awake) are grouped into sessions separated by gaps of more than 60 minutes, then assigned to the night labeled by the wake-up date within the sleep window (default 18:00 to 18:00).
- **FR-2**: For each night, the stage source is the highest priority source with sleep data for that night. In Bed intervals can come from another source.
- **FR-3**: The longest session of the night is the main sleep. Other sessions are naps and are listed separately.
- **FR-4**: Night statistics: bedtime, wake time, time in bed, total sleep, sleep efficiency (total sleep divided by time in bed), time to fall asleep when In Bed data exists, duration and percentage per stage, number of awakenings longer than 5 minutes.
- **FR-5**: Night detail: hypnogram, statistics, and when available during the night: heart rate, respiratory rate, oxygen saturation, sleeping wrist temperature deviation, heart rate variability.
- **FR-6**: Trends over a selectable period: total sleep per night against the sleep goal, stage composition, bedtime and wake time as ranges per night, average and variability of bedtime and wake time, weekday versus weekend comparison.
- **FR-7**: Nights from sources without stages show a single "Asleep" band and statistics that do not depend on stages.
- **FR-8**: Nights are recomputed when sleep samples, priorities, block rules or the sleep window change.

## UX notes

- Main view: last night's hypnogram at the top, then a list or calendar of nights, then trends.
- Swipe between nights on mobile.
- Hypnogram colors consistent with stage colors used elsewhere, accessible in both themes.

## Data and API notes

- `sleep_nights` table: user, night date, source, sessions, statistics, nap flag.
- Endpoints: `GET /api/v1/sleep/nights?from&to`, `GET /api/v1/sleep/nights/{date}`.
- Chart components: `Hypnogram`, `SleepRangeChart`, `TimeSeriesChart`.

## Edge cases

- Health Auto Export configured with summarized sleep: nights have totals but no hypnogram, and the page shows a hint to switch the automation to unsummarized sleep.
- Shift workers sleeping during the day: the sleep window preference handles it.
- Daylight saving time nights of 7 or 9 clock hours: durations computed in real elapsed time.
- Overlapping stage samples from the same source: the later sample wins for the overlapping interval.

## Acceptance criteria

- [ ] A night assembled from an XML export matches the Health app's totals for that night within 5 minutes.
- [ ] Changing the sleep source priority changes the night's stages after recomputation.
- [ ] Naps are excluded from nightly totals but visible.

## Future ideas

- Sleep debt over a rolling window.
- Correlations between sleep and next-day resting heart rate or HRV.
