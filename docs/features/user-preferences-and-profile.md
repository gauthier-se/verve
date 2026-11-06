# User preferences and profile

- **Status:** Planned
- **Milestone:** M2 (timezone, FR-3, in M1)
- **Related:** [Internationalization](platform-i18n.md), [Storage and aggregates](data-storage-and-aggregates.md), [Sleep page](page-sleep.md), [Workouts](page-workouts.md), [Energy expenditure](goals-energy-expenditure.md)

## Summary

Per-user settings that drive display and calculations: language, units, timezone, theme, week start, sleep window, and a profile (date of birth, sex, height, heart rate settings) needed for zones, BMI and energy estimates. Profile values are prefilled from Apple Health when available.

## User stories

- As a user, I want metric units but kilocalories, or imperial units with kilojoules.
- As a user, I want "today" and my nights to follow my timezone.
- As a user, I want my heart rate zones to use my real maximum heart rate.

## Scope

### In scope

- Display preferences, time preferences, profile, heart rate settings.
- Prefill from Apple Health data.

### Out of scope

- Per-device preferences (all preferences are stored server-side and follow the user).

## Functional requirements

- **FR-1**: Language: English or French (more as contributed), default from the browser.
- **FR-2**: Units: a unit system (`metric` or `imperial`) plus per-dimension overrides:

  | Dimension | Options |
  |---|---|
  | Weight | kg, lb, st |
  | Distance | km, mi |
  | Height | cm, ft and in |
  | Energy | kcal, kJ |
  | Temperature | degC, degF |
  | Elevation | m, ft |

- **FR-3** (M1): Timezone: IANA timezone, detected from the browser at first login and confirmed by the user. Changing it triggers recomputation of hourly and daily aggregates.
- **FR-4**: Week start: Monday or Sunday (default from locale). Used for weekly aggregates, weekly volume and check-in scheduling defaults.
- **FR-5**: Sleep window: start hour of the 24 hour window used to assign sleep to nights (default 18:00), nights labeled by wake-up date. Sleep goal duration (default 8 h).
- **FR-6**: Theme: light, dark or system.
- **FR-7**: Profile: date of birth, sex used for physiological formulas (`female`, `male`, `not specified`), height. When sex is not specified, formulas use the average of both variants and the UI says so.
- **FR-8**: Heart rate settings: maximum heart rate (manual, or estimated with the Tanaka formula `208 - 0.7 x age`, with a suggestion when a higher value has been observed in the last 12 months), resting heart rate source (30-day median of resting heart rate by default), zone method (percentage of maximum, Karvonen heart rate reserve, or custom boundaries).
- **FR-9**: Prefill: date of birth and sex from the XML `Me` element, height from the latest height sample. Prefilled values are marked "from Apple Health" until the user edits them. User edits are never overwritten by later imports.
- **FR-10**: Goals used across pages: daily steps goal, sleep goal, weekly exercise goal. Defaults: 10,000 steps, 8 h, 150 min.
- **FR-11**: Changing preferences takes effect immediately in the UI. Server-side computations depending on them are recomputed in the background.

## UX notes

- Settings page sections: Profile, Display, Time, Heart rate, Goals.
- Units preview next to the selector ("72.4 kg", "2,150 kcal").
- Timezone at first login (M1): when the user of the session has no timezone, the shell opens a dialog with the zone of the browser (`Intl.DateTimeFormat().resolvedOptions().timeZone`) in an editable field. Confirming stores it. "Not now" or Escape closes the dialog until the app loads again, and the days of the user stay in UTC until then.

## Data and API notes

- `user_preferences` and `user_profile` tables. A user has no `user_preferences` row until they set a preference, and the row is deleted with them.
- Endpoints: `GET/PATCH /api/v1/account/preferences` (timezone only in M1), `GET/PATCH /api/v1/account/profile`.
- `GET /api/v1/account/preferences` answers `{"timezone": "Europe/Paris"}`, with `null` until the user confirms one. `PATCH` takes the preferences to change, keeps the ones left out, and answers the preferences. A timezone that is not an IANA name answers 422 `timezone_invalid`, and so do the empty name and `Local`, which `time.LoadLocation` would read as UTC and as the zone of the server. `GET /api/v1/auth/me` includes the timezone, so that the web app knows at once whether to ask for it.
- Names are checked against the timezone database of the system, then the one embedded in the binary (`time/tzdata`), so that every name loads on a system without one, such as Windows. The `posix/` and `right/` trees that many systems install beside the IANA names answer 422 `timezone_invalid` too: the embedded database has neither, and `right/` counts leap seconds.
- `preferences.Location` gives the effective zone of a user for the computations: their timezone, or UTC until they confirm one.
- Values are always stored in canonical units, the SPA converts for display and input.

## Edge cases

- No date of birth: age-based estimates are disabled and pages that need them ask for it.
- No timezone yet (a user who has not logged in to the web app): aggregates use UTC until the user confirms one.
- Timezone changed while traveling: the UI suggests updating when the browser timezone differs from the preference for more than 24 hours, without changing it automatically.

## Acceptance criteria

- [ ] Every displayed value follows the selected units, including chart axes and tooltips.
- [ ] Changing the timezone moves the day boundaries of Today and daily charts.
- [ ] Prefilled profile values never overwrite manual edits.

## Future ideas

- Multiple saved locations or automatic timezone from travel.
