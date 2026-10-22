# Plans and weekly check-ins

- **Status:** Planned
- **Milestone:** M3
- **Related:** [Energy expenditure estimation](goals-energy-expenditure.md), [Notifications](notifications.md), [Body page](page-body.md), [Today page](page-today.md)

## Summary

A plan describes a body composition goal: cut, bulk, maintain or recomp, with a target rate of weight change. Verve turns it into daily calorie and macro targets based on the current maintenance estimate, tracks the trend weight against the planned trajectory and adherence to targets, and proposes adjusted targets at a weekly check-in that the user accepts or declines.

## User stories

- As a user starting a cut, I want to know how much to eat to lose 0.5 percent of my weight per week.
- As a user, I want my targets to adapt when my maintenance changes, but only with my approval.
- As a user, I want to see if I am on track and how consistent I am.

## Scope

### In scope

- Plan creation, targets, trajectory, adherence, weekly check-ins, plan history.
- One active plan per user.

### Out of scope

- Meal planning or food logging.
- Training programs.
- Plans for minors, pregnancy or medical conditions (the UI displays a disclaimer).

## Functional requirements

- **FR-1**: Plan types and default rates (percent of body weight per week):

  | Type | Default rate | Allowed range |
  |---|---|---|
  | Cut | -0.5 | -0.25 to -1.0 |
  | Bulk | +0.25 | +0.1 to +0.5 |
  | Maintain | 0 | 0 |
  | Recomp | 0 (calories at maintenance) | -0.25 to 0 |

- **FR-2**: A plan is defined by type, rate or target weight plus target date, start date and optional end date. When defined by target weight and date, Verve computes the required rate and warns when it falls outside the allowed range.
- **FR-3**: Initial maintenance comes from the current energy estimate (adaptive or sensor, see [Energy expenditure](goals-energy-expenditure.md)) or from a manual override.
- **FR-4**: Daily calorie target = maintenance + (weekly rate in kg x 7,700 / 7). A floor prevents targets below the estimated resting energy, with a warning.
- **FR-5**: Macro targets, all overridable by the user:
  - protein in g per kg of trend weight: 2.0 for cut, 1.8 for bulk, 1.6 for maintain and recomp, allowed range 1.6 to 2.2,
  - fat: 25 percent of calories by default, never below 0.6 g per kg,
  - carbohydrates: the remainder.
- **FR-6**: Trajectory: planned weight line from the trend weight at start with the target rate, shown against the trend weight and raw weigh-ins.
- **FR-7**: Adherence: per day, intake versus calorie target (on target within plus or minus 5 percent, over, under, not logged) and protein versus target. Weekly averages of calories and macros. Calendar view of adherence.
- **FR-8**: Weekly check-in on a configurable weekday (default: the user's week start). A background job prepares it with:
  - the updated maintenance estimate and its confidence,
  - the expected versus actual trend weight change over the week,
  - the average adherence of the week,
  - proposed new targets.
- **FR-9**: The check-in is presented to the user (and announced by notification when configured). The user accepts, declines, or edits the proposal. Accepted targets apply from the next day. Targets never change silently.
- **FR-10**: If a check-in is not answered within 7 days, it expires and current targets stay in place.
- **FR-11**: Plan events: target weight reached, end date reached, off track for 2 consecutive check-ins (actual rate outside half to double of the target). Each event can trigger a notification. On completion, Verve suggests a follow-up plan (for example maintenance after a cut).
- **FR-12**: Plan history keeps past plans with their check-ins and results (duration, total change, average rate, average adherence).
- **FR-13**: Only one plan can be active at a time. Starting a new plan ends the current one.

## UX notes

- Plan page: header with type, progress and next check-in date, trajectory chart, today's targets with consumed values, adherence calendar, check-in history.
- Plan creation wizard: choose type, see the current maintenance with method and confidence, pick rate or target, review targets, confirm.
- Check-in screen: compact summary and a clear accept or decline choice.
- Disclaimer on plan creation: not medical advice, consult a professional for aggressive goals or health conditions.

## Data and API notes

- `plans` and `plan_checkins` tables. Targets history stored per check-in.
- Job `plan.checkin` scheduled per user.
- Endpoints: `GET/POST /api/v1/plans`, `GET /api/v1/plans/active`, `POST /api/v1/plans/{id}/end`, `GET /api/v1/plans/{id}/checkins`, `POST /api/v1/plans/{id}/checkins/{checkinId}/decision`.

## Edge cases

- Not enough data for an adaptive estimate at check-in: the check-in uses the sensor estimate, says so, and may propose no change.
- Missing weigh-ins during the week: the check-in reports it and does not propose changes based on noise.
- User changes timezone or week start mid-plan: the next check-in date is recomputed.
- Recomp: progress is judged on body fat or waist trend when available rather than weight.

## Acceptance criteria

- [ ] Creating a cut at -0.5 percent per week for an 80 kg user with 2,600 kcal maintenance yields a target of about 2,160 kcal.
- [ ] A check-in never changes targets without an explicit acceptance.
- [ ] Adherence distinguishes unlogged days from days under target.
- [ ] Ending a plan keeps its history.

## Future ideas

- Diet breaks and refeed days scheduling.
- Calorie cycling between training and rest days.
- Progress photos (stored locally, never shared).
