Status: done

# 02: query, catalog, docs: a Source wins a day, not a decade

## What

- **`resolveSource` elects per day.** `internal/query/query.go:483` reads the
  distinct Sources over the window; it reads distinct `(day, source)` pairs
  instead and ranks each day's candidates with the same
  `catalog.ResolveSource`, which is unchanged:
  ```sql
  SELECT DISTINCT date(start_at) AS d, source
  FROM measurements
  WHERE account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?
  ```
  Manual is still split out before the election, exactly as today: it does not
  compete as a Source at any grain (ADR 0022).
  The index `measurements_account_metric_start` covers this the same way it
  covered the old one; the extra work is a `date()` per distinct row, not per row.

- **One winner everywhere is still one `source = ?`.** After ranking, if every
  day elected the same Source, `sourceFilter` carries that name and `where`
  emits today's predicate, argument for argument. That is every Account that
  exists right now, including the Watch-and-iPhone case `sourcePriority` was
  written for, so this milestone cannot change a single existing curve.

- **Several winners become contiguous runs, not a per-row lookup.** Sort the
  elected days, merge consecutive entries carrying the same winner into runs, and
  emit one range clause per run:
  ```sql
  ( (source = ? AND start_at >= ? AND start_at < ?)
    OR (source = ? AND start_at >= ? AND start_at < ?) )
  ```
  Merging across days with no data is safe: a day nobody recorded selects nothing
  whichever run swallows it. So the reference case, years of Apple with a
  three-month Google mirror inside them, is **three** clauses rather than four
  thousand, and the bounds stay
  RFC 3339 prefixes compared lexically against `start_at`, so the index is still
  usable. A pathological alternation costs one clause and three arguments per
  switch, bounded by the days in the window and far under SQLite's variable
  limit; it needs no fallback path, only the comment saying it was measured
  against the limit.

- **The Manual overlay composes unchanged.** In `sourceFilter.where`
  (`query.go:445`) the run predicate substitutes for `source = ?` wherever the
  winner appears, and the manual-days subquery is untouched:
  ```sql
  account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?
    AND (<runs> OR source = 'Manual')
    AND (source = 'Manual' OR date(start_at) NOT IN (…))
  ```
  Both mechanics now resolve at day grain, which is the first time they can be
  described in one sentence.

- **`reported()` names the dominant winner.** With several winners the Series
  still carries one Source name, ranked among the winners by the same priority.
  That rule already exists as `reportedSleepSource` (`sleep.go:281`); move it to
  one helper and have both call it, rather than writing it twice.

- **`sourcePriority` learns the real names** (`internal/catalog/priority.go`).
  Its entries are lowercase substrings, which is what makes this cheap. The
  reference Takeout reports `Apple Health Health Kit`, `Phone Health Kit`,
  `com.apple.heartratecoordinatord Health Kit`, `YAZIO Health Kit`,
  `Zepp Life Health Kit` and `Google Health App`, and **none of them contains
  "watch" or "iphone"**, so every one of them currently ranks last and ties are
  broken alphabetically. Two changes, existing patterns kept first so no current
  Account moves:
  ```go
  // A Source ending in "health kit" is Google's mirror of a HealthKit row: the
  // same measurement, one aggregator further from where it was taken. It ranks
  // behind the device that recorded it and ahead of nothing.
  "steps":                    {"watch", "iphone", "phone health kit", "health kit"},
  "distance_walking_running": {"watch", "iphone", "phone health kit", "health kit"},
  "flights_climbed":          {"watch", "iphone"},
  "sleep":                    {"watch", "iphone"},
  // New: a mirrored heart rate must never outrank the wrist that measured it.
  "heart_rate":               {"watch", "health kit"},
  "resting_heart_rate":       {"watch", "health kit"},
  ```
  `heart_rate` needs an entry precisely because it never had one: with no
  patterns every Source ties and "Apple Health Health Kit" wins on the letter A.

- **Nothing else changes grain.** Sleep already resolves per **Night**
  (`sleep.go:225`, ADR 0027) and stays as it is; a Night is this rule's shape for
  a span that crosses midnight. Sessions are entities and are resolved not at all
  (ADR 0028). Derived Metrics inherit, because each operand goes through this
  same filter. The Panel summary inherits for free, and that is the argument for
  day grain rather than bucket grain: `summarize` shares `where` with the bucket
  queries, so a month chart, a day chart and a headline figure are still provably
  reading the same rows.

- **Tests** in `internal/query/`: two Sources with disjoint day coverage return
  every day (the mirror case, which fails today by design); two Sources
  overlapping every day return exactly the higher-priority one's rows, byte for
  byte with the current expectation; a single Source emits the old predicate,
  asserted on the SQL string as `manualoverlay_test.go:201` already does for the
  overlay; a Manual entry on a day the second Source won still overlays; the
  summary of a two-Source window equals the summary computed over the same
  resolved rows, so the daily chart and the headline cannot disagree; a boundary
  day where the winner switches picks one Source for that whole day.

## Why here

ADR 0003 chose read-time resolution so the rule could change later without
touching stored data, and this is the later. Its "whole-range only" clause was
correct for what existed: two Apple devices worn on the same wrist over the same
years.

The reference Google Health Takeout breaks it, and not subtly. Its rows are
almost all HealthKit mirrors, so the same measurements arrive twice under two
names, and the mirror covers **three months** where the Apple export covers
years. `heart_rate` has no `sourcePriority` entry, the election falls back to
alphabetical order, and "Apple Health Health Kit" sorts before "Apple Watch de
Gauthier". Import both and a multi-year heart rate curve becomes a three-month
one, everywhere at once: the Panel, the Ledger, the summary, the co-variation.
The rows are all still in the database.

The priority patterns above fix that one Metric. Day grain is what stops the
next one from being a bug report: any Source that covers part of a history and
outranks the Source covering the rest wins the whole window today, and no
ordering of patterns can express "this one, but only where it has data".

Day grain rather than bucket grain is the load-bearing half of the decision, and
it is already argued in the code this issue edits: the resolved row set must not
depend on how the caller happens to be bucketing, or a monthly chart, a daily
chart and a summary of the same window can each read a different Source and
disagree about the number they show. Day is also the grain the other two
resolutions in this codebase use, for the same reason each time: it is the grain
at which the evidence actually changes.

This lands before the Connector, not after, because the alternative is shipping
an import that makes an Account's data provably worse the moment it finishes.

## Comments

Shipped as specified: `resolveSource` reads `(day, source)` pairs, elects per day,
merges consecutive days into runs, and keeps the single-winner predicate byte for byte
(`TestSingleWinnerKeepsTheOriginalPredicate` pins the string and the argument order).
`reportedSleepSource` now calls the shared `dominantSource`, so the two paths report by
one rule. ADR 0034 records it.

The run compression behaves as hoped on the shape that motivated it: years of one
export with a shorter mirror inside them come out as three range clauses, and
`TestRunsMergeAcrossDaysAndGaps` pins the merge across a day nobody recorded.

`sourcePriority` ended up more explicit than the sketch in this issue. `Phone Health
Kit` contains `health kit`, so a list of `{"watch", "iphone", "phone health kit",
"health kit"}` would have ranked the phone's mirror *above* the aggregate that includes
the watch, which is backwards for `steps`. The shipped list matches `apple health`
before `health kit`, a substring the phone's name does not contain, so the ordering
says what it means instead of relying on which pattern happens to match first.
