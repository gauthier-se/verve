# Body page

- **Status:** Planned
- **Milestone:** M4
- **Related:** [Energy expenditure](goals-energy-expenditure.md), [Plans and check-ins](goals-plans-and-check-ins.md), [User preferences and profile](user-preferences-and-profile.md)

## Summary

Body measurements over time: weight with its smoothed trend and rate of change, body fat, lean body mass, BMI and waist circumference, with the active plan's trajectory overlaid when relevant.

## User stories

- As a user, I want to see my real weight trend through daily fluctuations.
- As a user on a cut, I want to know my weekly rate of loss.
- As a user, I want to follow body fat and waist alongside weight.

## Scope

### In scope

- Weight, trend weight, rate of change, body composition, BMI, waist.
- Plan trajectory overlay.

### Out of scope

- Manual entry of measurements in Verve (measurements come from Apple Health).
- Progress photos.

## Functional requirements

- **FR-1**: Weight chart: raw weigh-ins as points, trend weight as a line (same smoothing as [Energy expenditure](goals-energy-expenditure.md)), optional active plan trajectory.
- **FR-2**: Rate of change: trend weight change over the last 7 and 28 days, in units and in percent of body weight per week.
- **FR-3**: Body fat percentage and lean body mass charts with their own trends.
- **FR-4**: BMI from samples when available, otherwise computed from trend weight and profile height, with the WHO categories shown as neutral reference bands.
- **FR-5**: Waist circumference chart when data exists.
- **FR-6**: Summary cards: current trend weight, change since a chosen date, lowest and highest trend weight over the period.
- **FR-7**: Period selector stored in the URL.

## UX notes

- Weight chart is the hero of the page, other sections below.
- Fluctuations explained in a short help text (water, food, timing).

## Data and API notes

- Reuses series endpoints and `GET /api/v1/body/trend?from&to` for trend weight.

## Edge cases

- Weigh-ins at different times of day: daily weight rule from [Energy expenditure](goals-energy-expenditure.md) FR-1.
- Two scales with offset readings: user can ignore one or use combine mode.
- Long gaps without weigh-ins: trend line interrupted rather than interpolated.

## Acceptance criteria

- [ ] Trend weight on this page equals the one used by plans and energy estimates.
- [ ] BMI is computed when no BMI samples exist but height and weight do.

## Future ideas

- Manual entry for measurements not available in Apple Health.
- Body measurements beyond waist (hips, chest, arms).
