Status: done

# 01: catalog: an entry for every Metric that really overlaps, and a way to say "last"

## What

### 1. `catalog.ResolveSource` gains a wildcard rank

The table can currently only *promote*. `rank` returns `len(patterns)` for a Source
matching nothing, so an unmatched Source always sorts after every matched one, and
there is no way to write "this named Source ranks **behind** whatever else turns up".

That is exactly what `body_mass` needs. The instrument is a scale, and scale names
are an open set (`Zepp Life` here, `Withings`, `Renpho`, `Garmin Index` elsewhere) —
enumerating them is the losing half of ADR 0011. What is closed and knowable is the
short list of apps that hold a *copy*: `Yazio` and `FatSecret` are food logs, and
neither ever weighed anything.

Add `"*"` as a pattern meaning **every unmatched Source ranks at this position**:

```go
// "*" is the position of everything this table does not name. Without it a
// pattern list can only promote, and the thing worth saying about a body mass is
// that a food log holding a copy ranks *behind* whatever scale actually weighed
// it — including a scale whose name we have never seen.
```

In `rank`, the wildcard must not swallow a Source a later pattern names, so it
resolves after the others rather than in the loop:

```go
rank := func(source string) int {
	lower := strings.ToLower(source)
	wildcard := len(patterns)
	for i, p := range patterns {
		if p == "*" {
			wildcard = i
			continue
		}
		if strings.Contains(lower, p) {
			return i
		}
	}
	return wildcard
}
```

A list with no `"*"` keeps `len(patterns)` and behaves byte for byte as today, which
is every entry that exists now.

### 2. The missing entries

```go
// --- Energy ---
// The Watch measures an all-day figure. Yazio, Nike Run Club and Strava each write
// back the energy of one workout the Watch has already counted inside that figure,
// so they are not rival measurements of the same quantity — they are a subset of it,
// and electing one replaces a day with a run. On the reference Account the daily
// means on overlapping days are Watch 1043, Strava 747, Nike 562, Yazio 521,
// iPhone 410: a factor of 2.5 currently decided by sort.Strings.
"active_energy":       {"watch", "iphone", "apple health", "health kit"},
"basal_energy":        {"watch", "iphone", "apple health", "health kit"},
"apple_exercise_time": {"watch", "iphone"},
"apple_stand_time":    {"watch", "iphone"},

// --- Body composition ---
// The scale is the instrument and its name is an open set, so the wildcard holds
// the position of "some scale" and the food logs that mirror it rank behind.
"body_mass":           {"*", "yazio", "fatsecret", "health kit"},
"body_mass_index":     {"*", "yazio", "fatsecret", "health kit"},
"body_fat_percentage": {"*", "yazio", "fatsecret", "health kit"},
"lean_body_mass":      {"*", "yazio", "fatsecret", "health kit"},

// --- Nutrition ---
// Two food logs, and neither is closer to the food than the other: this is the one
// group where the tie-break is genuinely arbitrary rather than accidentally right.
// It is ranked anyway so that a day logged in both does not depend on the alphabet,
// and day-grain election (ADR 0034) already keeps the loser's own days intact.
"dietary_energy": {"yazio", "fatsecret", "health kit"},
// …and the same list for every other dietary_* Metric a Connector can emit.

// --- Watch-measured, mirrored elsewhere ---
"vo2_max":           {"watch", "apple health", "health kit"},
"oxygen_saturation": {"watch", "iphone", "apple health", "health kit"},

// Two devices playing audio at different times is genuinely *additive*, and
// electing one discards the other's hours. Merging complementary Sources is where
// ROADMAP has it; until then the Watch is the better of two wrong answers and this
// entry is where that note lives.
"headphone_audio_exposure": {"watch", "iphone"},
```

The `dietary_*` list is long enough to be worth generating rather than typing: every
Catalog slug with the `dietary_` prefix takes the same patterns, built once in an
`init` or a small loop beside the literal, so a nutrient added to the Catalog later
cannot be forgotten here.

### 3. A test that notices the table falling behind

`TestResolveSourceNoPriorityFallsBackAlphabetical` no longer tests what it is named
after: it passes `heart_rate`, which gained an entry in ADR 0034, and
`"Apple Watch"` now wins by matching `"watch"` at rank 0 rather than by the
alphabet. Point it at a slug with no entry, or the fallback is untested.

Then the check the table actually lacked — the alphabet must never decide a real
overlap:

- **`TestEveryOverlappingMetricIsRanked`**, over the Metrics a Connector can emit:
  for each, assert that if two plausible Source names for it exist, they do not tie.
  Concretely, a table of `(slug, []string{sourceA, sourceB})` drawn from the
  reference export's real Source names, asserting `rank(a) != rank(b)`.
- **`TestWildcardRanksBetweenNamedPatterns`**: `ResolveSource("body_mass",
  []string{"Yazio", "Zepp Life"})` returns `"Zepp Life"`, and
  `ResolveSource("body_mass", []string{"Yazio", "FatSecret"})` returns `"Yazio"`.
- **`TestWildcardAbsentIsUnchanged`**: an entry with no `"*"` ranks exactly as
  before, so the existing seven are provably untouched.

## Why it is worth doing now rather than when it breaks

Because when it breaks it will not look broken. ADR 0034 opened on precisely this
shape: a Source was elected, its rows were served, and the number on screen was a
quarter of the truth with nothing warning. Here the trigger is smaller than
importing a second export — renaming a watch in iOS Settings is enough. Drop the
`Apple` prefix and `active_energy` flips to a run-tracking app on the days those
apps wrote a workout, a ~480 kcal/day swing on a figure `total_energy_expenditure`
and the whole energy-planning surface sit on top of.

## Out of scope

Merging complementary Sources (ROADMAP). This issue ranks; it does not add.

## Comments

**Implemented.** `catalog.SourceWildcard` (`"*"`) is the new pattern, resolved after
the named ones inside `rank` so it cannot swallow a Source a later pattern names —
`TestWildcardDoesNotSwallowALaterPattern` is that trap written down.

Verified against the reference database, which is where it matters:

| Metric | Before | After |
| --- | --- | --- |
| `body_mass` | `Yazio` | **`Zepp Life`** |
| `body_fat_percentage` | `Yazio` | **`Zepp Life`** |
| `active_energy` | `Apple Watch …` (by the alphabet) | `Apple Watch …` (by the rule) |
| `steps`, `basal_energy`, `vo2_max`, `apple_exercise_time` | `Apple Watch …` | unchanged |
| `dietary_energy` | `FatSecret` on the 3 shared days | `Yazio` |

So the body-composition Metrics were being read off a food log's copy rather than off
the scale that took the measurement, and now are not. Everything else keeps the answer
it had, which is the point: the change makes the existing answers *derived* rather
than lucky.

Three notes on the implementation:

- **The `dietary_*` entries are built in an `init` from the Catalog**, not typed. The
  table fell behind because nothing forced it to keep up, and forty hand-written lines
  would have reproduced exactly that failure the next time a nutrient is added.
  `TestEveryDietaryMetricIsRanked` asserts the loop actually covers them.
- **`total_energy_burned` was added too**, which the issue did not list: it is the
  same Watch-versus-app overlap as `active_energy`, one Metric along.
- **Two stale doc comments fixed while in the file.** `ResolveSource` still said
  "Whole-range only — per-bucket resolution is deferred (ADR 0003)", and the
  `SourceManual` comment still described a whole-range election. Both have been
  false since ADR 0034.

`TestResolveSourceNoPriorityFallsBackAlphabetical` now points at `respiratory_rate`
and asserts up front that the slug is unranked, so the next time someone ranks it the
test fails loudly instead of silently testing the priority path it was written to
avoid.

`TestEveryOverlappingMetricIsRanked` is the drift guard: 22 real Source pairs from the
reference exports, each asserted not to tie.
