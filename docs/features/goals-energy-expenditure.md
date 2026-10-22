# Energy expenditure estimation

- **Status:** Planned
- **Milestone:** M3
- **Related:** [Plans and check-ins](goals-plans-and-check-ins.md), [Body page](page-body.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [User preferences and profile](user-preferences-and-profile.md)

## Summary

Verve estimates maintenance calories (total daily energy expenditure) with two methods. The sensor method adds basal and active energy measured by the Apple Watch. The adaptive method compares calories consumed (logged in a nutrition app that writes to Apple Health) with the smoothed weight trend, which is more reliable. The adaptive estimate is the reference whenever enough data exists, and the UI always shows which method is used and how confident it is.

## User stories

- As a user, I want to know my real maintenance calories, not a formula from a website.
- As a user who logs food and weighs in regularly, I want an estimate that adapts to what actually happens to my weight.
- As a user without enough data yet, I want a reasonable estimate in the meantime.

## Scope

### In scope

- Trend weight computation.
- Sensor and adaptive estimates, method selection, confidence.
- Daily estimates history and maintenance trend chart.

### Out of scope

- Logging food in Verve (food comes from Apple Health).
- Medical or clinical use.

## Functional requirements

- **FR-1**: Daily weight: from body mass samples after source selection, the first weigh-in of the day by default (configurable: first of the day or daily mean).
- **FR-2**: Trend weight: exponentially smoothed moving average of daily weights (smoothing factor 0.1 per day by default), handling missing days by decaying according to elapsed time rather than inventing values.
- **FR-3**: Daily intake: daily sum of dietary energy consumed after source selection. Macros (protein, carbohydrates, fat, fiber) are read the same way.
- **FR-4**: A day is considered logged when its intake is above a plausibility floor (default 800 kcal). Days below are treated as unlogged, not as low-intake days. Users can mark a day as "incomplete log" to exclude it, or force a day to count.
- **FR-5**: Sensor estimate for a day: basal energy burned plus active energy burned. When basal energy is missing, fall back to the Mifflin-St Jeor resting energy estimate from profile and trend weight.
- **FR-6**: Adaptive estimate over a rolling window (default 28 days, minimum 14):
  `maintenance = mean intake over logged days - (trend weight at window end - trend weight at window start) x energy density / days in window`,
  with an energy density of 7,700 kcal per kg by default (configurable).
- **FR-7**: The adaptive estimate is available when the window contains at least 14 logged days, at least 80 percent of days logged, and at least 4 weigh-ins. Otherwise the sensor estimate is used.
- **FR-8**: Confidence level (`high`, `medium`, `low`) derived from logging coverage, number of weigh-ins and weight noise (residuals around the trend). Shown with a short explanation of what would improve it.
- **FR-9**: Daily estimates are stored with date, method, value, confidence and inputs summary (days logged, weigh-ins, trend weights). They are recomputed when underlying data changes.
- **FR-10**: The maintenance page shows: current estimate with method and confidence, both methods side by side when available, maintenance over time, intake versus maintenance chart, trend weight with raw weigh-ins.
- **FR-11**: Users can enter a manual maintenance override, used by plans until removed.

## UX notes

- Plain-language explanation of each method, with a note that the Apple Watch tends to overestimate active energy.
- Clear "not enough data yet" state listing what is missing (for example "6 more logged days needed").

## Data and API notes

- `energy_estimates` table.
- Job `energy.daily` runs after nutrition, weight or energy data changes and nightly.
- Endpoints: `GET /api/v1/energy/estimates?from&to`, `GET /api/v1/energy/current`, `PUT /api/v1/energy/override`.

## Edge cases

- Large water weight swings (high-carb refeed, travel): smoothing reduces the impact, confidence drops when residuals are high.
- Nutrition app that writes intake in several entries per meal: summed per day.
- Two nutrition apps writing the same meals: source priority selects one.
- Weight loss from illness without logging: unlogged days are excluded, and the window may not qualify for adaptive.
- Energy density for lean mass gain differs from fat loss: the default is a documented simplification, configurable by advanced users.

## Acceptance criteria

- [ ] With synthetic data (constant 2,500 kcal intake, stable weight for 28 days), the adaptive estimate is 2,500 kcal plus or minus 50.
- [ ] With synthetic data losing 0.5 kg per week at 2,000 kcal intake, the estimate is about 2,550 kcal.
- [ ] Days below the plausibility floor are excluded from the intake mean.
- [ ] The UI always shows which method produced the current estimate.

## Future ideas

- Calibrated daily expenditure: sensor values scaled by the ratio between adaptive and sensor estimates over the window, for day-to-day variation.
- Energy density adapted to the phase (cut or bulk).
