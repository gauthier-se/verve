# PRD: the priority table covers four Metrics and the alphabet decides eighteen

## Goal

ADR 0003 elects one winning Source per Metric; ADR 0034 moved that election to day
grain so a short mirror cannot take a long history. Both are right and both are
implemented. What neither addressed is **how thin `sourcePriority` actually is**:
it holds seven entries, and the reference Account carries twenty-three Metrics with
more than one Source.

For the other eighteen, `ResolveSource` finds no matching pattern, every candidate
ties at rank `len(patterns)`, and the winner is decided by `sort.Strings`. On this
Account the alphabet happens to return the right answer almost everywhere, for one
reason: Apple names its devices `Apple Watch de Gauthier`, and `A` sorts before
`FatSecret`, `Nike Run Club`, `Strava`, `Yazio` and `iPhone`. The correct behaviour
is an accident of a vendor's naming convention.

The comment on `sourcePriority` says an entry is only needed for "Metrics prone to
harmful overlap". That is the right rule. The table simply does not satisfy it: the
Metrics below overlap on real days, in the reference export, today.

## The evidence

Counted over the reference Account's whole history, days where two or more Sources
recorded the same Metric:

| Metric | Sources | Overlapping days | Winner today | Decided by |
| --- | --- | --- | --- | --- |
| `active_energy` | 5 | 777 / 1316 | `Apple Watch …` | alphabet |
| `body_mass` | 4 | 356 / 577 | `FatSecret`, then `Yazio` | alphabet |
| `body_mass_index` | 2 | 350 / 560 | `Yazio` | alphabet |
| `body_fat_percentage` | 2 | 349 / 504 | `Yazio` | alphabet |
| `vo2_max` | 2 | 66 / 327 | `Apple Watch …` | alphabet |
| `apple_exercise_time` | 2 | 19 / 414 | `Apple Watch …` | alphabet |
| `apple_stand_time` | 2 | 19 / 413 | `Apple Watch …` | alphabet |
| `dietary_*` (8 Metrics) | 2 | 2-3 / ~850 | `FatSecret` | alphabet |
| `oxygen_saturation` | 2 | 3 / 412 | `Apple Watch …` | alphabet |
| `headphone_audio_exposure` | 2 | 2 / 1248 | `Apple Watch …` | alphabet |
| `basal_energy` | 2 | 1 / 1315 | `Apple Watch …` | alphabet |
| `steps` | 2 | 413 / 1420 | `Apple Watch …` | **priority** |
| `distance_walking_running` | 5 | 443 / 1420 | `Apple Watch …` | **priority** |
| `flights_climbed` | 2 | 406 / 1398 | `Apple Watch …` | **priority** |
| `heart_rate` | 2 | 193 / 414 | `Apple Watch …` | **priority** |

`active_energy` is the sharpest case. On the 777 days where several Sources report
it, their daily means are:

| Source | Days | Mean kcal |
| --- | --- | --- |
| `Apple Watch de Gauthier` | 389 | 1043 |
| `Strava` | 2 | 747 |
| `Nike Run Club` | 31 | 562 |
| `Yazio` | 752 | 521 |
| `iPhone de Gauthier` | 389 | 410 |

A factor of 2.5 between the candidates, and the `sort.Strings` tie-break picks
between them. The three app Sources are not competing measurements of the same
quantity: `Yazio`, `Nike Run Club` and `Strava` re-broadcast the energy of a single
workout into HealthKit, where the Watch already counted it as part of an all-day
figure. Election is the right mechanic and the Watch is the right winner; nothing
in the code says so.

## What breaks it

Nothing in the data. A rename.

Open iOS Settings and call the watch `Watch de Gauthier` rather than `Apple Watch
de Gauthier`. `W` (0x57) still sorts before `Yazio` and `iPhone`, so most days are
unchanged and nothing looks wrong. But `N` (Nike Run Club) and `S` (Strava) now sort
first, so on the 33 days those apps wrote a workout, `active_energy` silently drops
from an all-day 1043 kcal to a single run's 562. The Panel, the Ledger, the summary
and `total_energy_expenditure` all follow, and nothing warns, because from the
engine's point of view a Source was elected and its rows were served. That is the
exact failure ADR 0034 was written against, one layer down: it fixed the *grain* of
the election, not the *thinness* of the ranking that election runs.

The second Connector makes it ordinary rather than hypothetical. Google Health's
`Health Kit` suffix is already ranked for `steps`, `distance`, `heart_rate` and
`resting_heart_rate` — and for nothing else. A Takeout carrying a mirrored
`active_energy` or `body_mass` enters this table unranked.

## The concept

**A Metric with more than one Source in any real export needs an explicit entry.**
Not a new mechanic: `sourcePriority` already expresses exactly this, and the fix is
to populate it against the evidence rather than against the two Metrics that were
noticed first. Ranking stays a closed, hand-curated table of lowercase substrings —
that is what ADR 0009 keeps compiled in, and the substring match is already
load-bearing for Apple's non-breaking space.

**The ranking principle is provenance distance, and it is already written down.**
ADR 0034 named it for Google: a Source one aggregator further from the device that
took the measurement ranks behind one that recorded it natively. `Yazio` holding a
body mass the `Zepp Life` scale measured is the same species as `Apple Health
Health Kit` holding a heart rate the Watch measured. The device that took the
reading wins; the app that mirrored it ranks behind; an app that only ever sees a
subset of the quantity (a run, not a day) ranks behind that.

**Scope, and what stays deferred.** This is ranking only. Merging complementary
Sources stays where ROADMAP has it, and `headphone_audio_exposure` is the reminder
that it is a real gap: two devices playing audio at different times is genuinely
additive, and electing one throws away the other's hours. Ranking it is still
strictly better than the alphabet in the meantime, and the entry is where the day's
note about it belongs.

**A test that fails when the table falls behind, not one that pins today's answer.**
The table drifted because nothing could notice it drifting. The unit that catches
that is not another case in `priority_test.go` — it is a check over the reference
fixture that no Metric resolves a multi-Source day by tie-break.
