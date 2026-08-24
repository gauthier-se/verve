# Source resolution is per day

## Context

ADR 0003 keeps every Source and resolves overlap at read time, electing one
winning Source per Metric. It closed with a scope line:

> Whole-range only, per-bucket resolution is deferred (ADR 0003).

That was correct for the data it was written against: an Apple Watch and an
iPhone, worn by the same person over the same years, both counting steps. Two
Sources with the same span, where picking one for the window and picking one per
day give the same answer.

A second Connector breaks it, and the reference Google Health Takeout breaks it
in the plainest possible way. A Google Health account is usually filled by
syncing another platform into it, so the archive is mostly a copy of an Apple
export: the same measurements, one aggregator further from the device, under
Source names like `Apple Health Health Kit` and `Phone Health Kit`. In the
reference export it covers three months of a history that runs for years.

`heart_rate` had no `sourcePriority` entry, so every Source tied and the winner
was decided alphabetically. `Apple Health Health Kit` sorts before
`Apple Watch de Gauthier`. Import both exports and the winner of a five-year
window is a Source that holds three months of it: the Panel, the Ledger, the
summary and the co-variation all collapse to a quarter, with every row still in
the database. Nothing warns, because from the engine's point of view a Source was
elected and its rows were served.

Priority patterns fix that one Metric. They cannot fix the shape of the rule: no
ordering of names can express "this Source, but only where it has data", and the
next Account to hold one export inside another's span hits the same wall.

## Decision

**Elect the winning Source per day.** `resolveSource` reads the distinct
`(day, source)` pairs over the window, ranks each day's candidates with the same
`catalog.ResolveSource`, and the read filters on the winner *of that day*.

**Day grain, not the caller's bucket grain.** The resolved row set must not
depend on how the caller happens to be bucketing, or a daily chart, a monthly
chart and a window summary could each read a different Source and disagree about
the number they show. Day is also the grain the two other resolutions in this
codebase already use: the Manual overlay (ADR 0022) replaces a day at a time, and
sleep resolves per **Night** (ADR 0027), which is what day grain means for a span
that crosses midnight. Whole-range election was the odd one out.

**One winner everywhere keeps the old predicate, byte for byte.** When every day
elects the same Source the filter is the `source = ?` equality it has always
been, with the same arguments in the same order. That is every Account holding a
single export, including the Watch-and-iPhone case the original rule was written
for, so this changes no curve that exists today.

**Several winners become contiguous dated runs.** Consecutive days electing the
same Source merge into one half-open instant range, and merging across days
nobody recorded is safe: a day with no rows selects nothing whichever run spans
it. The reference case, years of one export with a shorter mirror inside them,
is three ranges rather than four thousand, and the bounds stay RFC 3339 prefixes
compared against `start_at`, so the `(account_id, metric, start_at)` index still
serves the query.

**This is still election, not merging.** A day both Sources cover yields one of
them, so nothing is double counted. Combining complementary Sources within a day
remains deferred, exactly as ADR 0003 left it.

## Considered Options

- **Per day, with a whole-window fast path (chosen).** One extra `date()` per
  distinct pair at resolution time, a predicate that grows only when the answer
  actually varies, and no change for any existing Account.
- **Keep whole-range election and rank the new Sources properly.** Cheapest, and
  it does fix the reference Account's heart rate. Rejected as the whole answer:
  it treats a structural problem as a naming problem. A correctly-ranked Source
  that covers part of a history still wins the years it never saw.
- **Per bucket, at whatever grain the caller asked for.** Follows the request
  more literally and is worse: the same window read at day and at month grain
  could resolve to different Sources, and the Panel summary, a single bucket
  spanning the range (ADR 0019), would elect one Source for a headline figure
  and another for the curve under it. The invariant that makes the Manual overlay
  safe is precisely that it does not do this.
- **Merge complementary Sources instead of electing.** The real answer for two
  devices with genuinely disjoint coverage, and out of scope here: it needs a
  rule for what to do when they overlap partially within a day, which is a
  feature, not a grain change. ROADMAP keeps it.
- **Normalize Source names at import so a mirror and its original become one.**
  Would remove the overlap entirely by pretending two provenance claims are one.
  Rejected: a row Google copied and a row Apple recorded are different facts, and
  a rewrite at import is not reversible.
- **Let the Account pick a Source per Metric.** A settings surface for a question
  the data can answer, and one more thing to maintain as devices come and go.

## Consequences

- A Series still carries one `source` name. With several winners it is the
  dominant one, ranked by the same priority the per-day election used, the rule
  sleep already reported by, now shared by both paths as `dominantSource`.
- `sourcePriority` gains entries for `heart_rate`, `resting_heart_rate` and
  `distance`, and Google's mirrored names rank behind natively-recorded ones. A
  Source matching no pattern still ranks last and ties still break
  alphabetically, so the fallback is unchanged; it is simply no longer load
  bearing for the Metrics people actually hold twice.
- A `start_at` no Connector could parse has no day, so it takes no part in the
  election. It took no part in any bucket either (`date()` is NULL for it), so
  this loses nothing that was ever drawn.
- The Manual overlay composes with the runs rather than beside them: the winner
  clause is substituted where the single equality used to sit, and the
  manual-days subquery is untouched. Both mechanics now resolve at the same
  grain, which is the first time they can be described in one sentence.
- Resolution costs one row per `(day, source)` pair instead of one per Source.
  For a Metric with one Source over a year that is 365 rows scanned from an index
  that already had to be walked, and the alternative was a query that answered a
  different question.
