Status: done

# 01: query, api: one date, everything on it, in one call

## What

- **`internal/query/day.go`**, a read module composing what already exists. It
  derives nothing that another module owns:

  ```go
  // DayDetail is one calendar date read as an index: what the Account holds on it,
  // and the per-day Source election ADR 0034 already performs. A Day is a Bucket
  // and not an entity, so it carries figures and references, never a Trace
  // (ADR 0041).
  type DayDetail struct {
      Date        string            `json:"date"`
      Metrics     []DayMetric       `json:"metrics"`
      Night       *NightSummary     `json:"night,omitempty"`
      Sessions    []data.Session    `json:"-"` // mapped to sessionView by the handler
      Annotations []data.Annotation `json:"-"`
      Manual      []data.Measurement `json:"-"`
      Exclusions  []data.Exclusion  `json:"-"`
      Phase       *data.Phase       `json:"-"`
  }

  // DayMetric is one Metric's figure for the date, folded by that Metric's own
  // rule so it is the same number the bar above the same date draws.
  type DayMetric struct {
      Metric      string              `json:"metric"`
      Unit        string              `json:"unit"`
      Aggregation catalog.Aggregation `json:"aggregation"`
      // Value is nil for a gap and never 0: a day with no steps recorded is not a
      // day with no steps (ADR 0014).
      Value    *float64 `json:"value,omitempty"`
      // Source is the Source elected for this Metric on this date (ADR 0034), which
      // is the grain the election already runs at, so this field costs no new rule.
      Source   string   `json:"source,omitempty"`
      Pinned   bool     `json:"pinned,omitempty"`
      // Excluded distinguishes a refusal from a gap: an Exclusion covering this
      // date is why the value is absent, and no other screen can say so (ADR 0033).
      Excluded bool     `json:"excluded,omitempty"`
  }
  ```

- **The metric rows come from the engine, not from a DISTINCT.**
  `MetricsWithData` grows a window: `MetricsWithDataIn(ctx, accountID, from, to)`
  carries the implementation and `MetricsWithData` delegates with an open one.
  A second day-scoped function is the wrong shape, because this one exists
  precisely so that a caller never has to know sleep is States and training is
  Sessions, and a copy would forget both on the day a third family arrives
  (`metadata.go`, ADR 0037).

- **Each figure is a one-day `Request` through the existing path.** Same
  `Engine.Series` rule, same `resolveSource`, and `sourceFilter.reported()`
  names the winner: over a one-day window the election has exactly one answer,
  so the Source field is read off machinery that is already there rather than
  computed again.

- **`NightSummary` is extracted, not duplicated.** `NightDetail` becomes
  `struct { NightSummary; Intervals []NightInterval }`, so the figures have one
  definition and `Engine.Night` stays the only thing that resolves a night. The
  Day carries the summary and no intervals: the shape is a link, not a payload.
  `/v1/nights/{date}` keeps its wire shape but for field order, which nothing
  reads.

- **The Night on a Day is the one labelled by that date** (ADR 0027): the night
  that woke into that morning, which is the night the sleep Panel already draws
  on it. Absent night, absent field.

- **The Sessions are the ones that started on that date**, with none of the
  Night's noon shift, exactly as ADR 0040 folds a bucket. `[dayStart, nextDay)`
  on `start_at`, the same bounds `source.go` already spells.

- **`GET /v1/days/{date}`** in a new `internal/api/dayhandlers.go`, behind
  `requireAuth`. The handler maps the domain objects onto the views that already
  exist, `sessionView`, `annotationView`, `exclusionView` and
  `measurementResponse`, so a workout on the Day is byte-identical to the same
  workout in the list and `types.ts` gains a `Day` composed of types it already
  holds.

- **An empty date is a 200 with empty collections**, never a 404. A date exists
  whether or not anything happened on it, which is the sharpest difference
  between a Day and every other addressable thing in Verve. A malformed date is
  the validator's 422, and another Account's data is simply absent.

- **The payload's contract** goes in `web/src/lib/types.ts` and into
  `contract_test.go`'s list, like every other payload the SPA reads.

- **Tests** in `internal/query/day_test.go` and
  `internal/api/dayhandlers_test.go`:
  - a day's figure equals the same day's Point from `/v1/series`, asserted
    directly, so the two paths are pinned to each other the way the Night and
    the sleep Metric already are;
  - a pinned Metric with no data on the date is present with a nil value, and an
    unpinned one with no data is absent entirely;
  - two Sources on one date: the elected one is reported, and a date where the
    election flips relative to its neighbour reports its own winner and not the
    window's;
  - the Night shown for `2026-03-03` is the night that woke on the 3rd, asserted
    against a State that starts on the 2nd at 23:14;
  - a workout starting 23:40 on the 3rd belongs to the 3rd and not the 4th;
  - an Exclusion covering the date marks its Metric `excluded` and leaves the
    value nil, distinguishably from an unexcluded gap;
  - an empty date is a 200 with empty collections; a malformed one is 422;
  - a Manual row on the date comes back with its id, so the page can delete it.

## Why here

**One call rather than six** because the composition rules are read-path
decisions with one right answer each: which Night belongs to this date, which
side of midnight a workout falls on, whether an absent row is a gap or a
refusal. Assembled client-side, those three answers live in a component and
drift from the engine that made them. `GET /v1/plan` already set this precedent,
deriving its targets server-side so the client never re-computes.

**The read is a module and not a handler** (ADR 0037), which is what makes the
interesting assertions cheap: "the Day figure equals the Series Point" is a test
against two functions, not two HTTP round trips through a migration and an
argon2id hash.

**The Source field is the reason the page is defensible at all.** ADR 0034
already elects per day; nothing displays it. This issue adds no rule, it
surfaces one, and that is the difference between a Day page and a dashboard of
convenience.

**No new grain enters the engine.** Every figure here is a day bucket that
`/v1/series` can already produce, requested for one day. There is no parameter
in this payload that could be widened into the sub-day Series contract ADR 0012
refuses.

## Comments

Shipped. `internal/day/day.go` with `day_test.go`, `internal/api/dayhandlers.go`
with `dayhandlers_test.go` and its route, `NightSummary` split out of
`NightDetail` in `internal/query/night.go`, `MetricsWithDataIn` in
`internal/query/metadata.go` (with `hasStates` and `hasTrainingData` windowed to
match), `ListManualOn` in `internal/data/measurement.go`, the `Day`, `DayMetric`
and `NightSummary` types in `types.ts` pinned by the contract test, ADR 0043 and
the **Day** entry in CONTEXT.md.

Five things came out of the implementation that the spec did not anticipate:

- **The module is `internal/day`, not `internal/query/day.go`.** The spec put it
  in the read engine; the codebase's own precedent says otherwise, and says why in
  as many words. `internal/history` is a separate package because "the events are
  Imports, Phases, Exclusions and Annotations, four tables the read engine never
  aggregates and has no business naming. Composing the two is neither module's
  job". A Day is that shape exactly, with five tables instead of four. Putting it
  in `internal/query` would have made the read engine import nothing new and know
  about everything.
- **The figure goes through `Engine.Series`, not through `resolveSource` and a
  fold.** The spec described reusing the election and naming the winner with
  `sourceFilter.reported()`, which is what `Series` already does on the way past.
  Asking for the whole one-day Series instead gets the Source, the Manual overlay
  and the Metric's rule in one call, and it covers the four shapes the spec's
  measurement-flavoured description did not: a derived Metric, sleep from States,
  and the two training-volume Metrics from Sessions. One day at day grain is at
  most one Point, so the figure is `Points[0]` or a gap.
- **`Read` carries domain rows and the handler maps them.** `dayView` in
  `internal/api` is the wire type, built from the `sessionView`, `annotationView`,
  `exclusionView` and `measurementResponse` that already exist. The alternative,
  a module owning its own JSON like `internal/history` does, would have meant a
  second definition of what a workout looks like on the wire, which is the one
  thing this page must not introduce.
- **The Exclusion check asks the rule rather than restating it.** `covers` builds
  a single-entry `data.ExclusionSet` and calls `Excludes`, because that function
  is the Go twin of the SQL purge and the two are pinned to each other by test. A
  fresh date comparison here would have been a third answer waiting to disagree
  with the other two.
- **The workouts list is reversed and capped.** The model lists newest first,
  which is right for a browse and wrong for a Day, which is a chronology. The cap
  (200) is not in the spec: a day past it is not a day, and the page should not be
  unbounded because an import was.

One thing found and deliberately not fixed: **the History page silently drops
every Phase band.** `handleOpenPhase` writes `started_at` as RFC 3339, and
`internal/history` parses it with the day layout and `continue`s on failure, so
the parse never succeeds in a real instance. The tests miss it because they seed
day labels. It is filed separately; this module reads both shapes through its own
`dayOf`, so the Day page is correct either way.

The docs split in the PRD was inconsistent: the Docs section put ADR 0043 in
`03`'s docs pass while this issue's own What line claimed it. The ADR landed here,
with the decision it records, the way ADR 0041 landed with issue `01` of the
intra-day milestone. `03` keeps the README, the ROADMAP row and the reciprocal
clause on CONTEXT.md's **Night** entry, all of which are about navigation and
belong with the links.

Two tests earn their place beyond the spec's list: a Manual entry appears in the
figures as well as in its own section (it is data on the day, not only a row you
can delete), and a date before any Phase opened carries none, which is the
absence the phase lookup gets wrong if it reaches for `Current` instead.
