Status: done

# 01: query, api: a Night is addressable, and it carries what folding destroys

## What

- **`internal/query/night.go`**, beside `sleep.go` and reading through it:
  ```go
  // NightDetail is one Night's shape and the figures that need its axis.
  type NightDetail struct {
      Night     string           `json:"night"`      // the morning it woke on
      Source    string           `json:"source"`     // the Source elected for it
      Intervals []NightInterval  `json:"intervals"`  // in start order
      Asleep    float64          `json:"asleep"`
      Awake     *float64         `json:"awake,omitempty"`
      Onset     *string          `json:"onset,omitempty"`
      Wake      *string          `json:"wake,omitempty"`
      Efficiency *float64        `json:"efficiency,omitempty"`
      // Basis names the denominator behind Efficiency, because the classic one
      // (time in bed) is unavailable on a Watch-only night and a percentage
      // whose denominator is assumed is a number that lies quietly.
      EfficiencyBasis string     `json:"efficiency_basis,omitempty"`
      Latency   *float64         `json:"latency,omitempty"`
  }
  ```
- **It reuses the Metric's resolution, it does not repeat it.** `sleepRows` for a
  one-day window, then `resolveNights` (`sleep.go`), then the intervals of the
  winning Source. The in-bed fallback and the per-Night Source election (ADR
  0027) therefore hold identically, and the page cannot disagree with the bar on
  the same night. A second implementation is the failure this issue exists to
  avoid, so if `resolveNights` needs a small signature change to hand back its
  rows, that change is the right one.
- **The figures, each absent rather than zero when unevidenced**:
  - `Asleep` is the Point's value for that night, by construction;
  - `Onset` and `Wake` are the first `asleep*` start and the last `asleep*` end,
    RFC 3339, absent on an in-bed-only night;
  - `Awake` is the `awake` minutes falling between them. Awake time before onset
    or after waking is not fragmentation, it is being up;
  - `Efficiency` is `Asleep ÷ (Wake − Onset)` as a percentage with
    `EfficiencyBasis: "onset_to_wake"`, and `"time_in_bed"` with the in-bed span
    as the denominator when in-bed rows exist. Nil when there is no onset;
  - `Latency` is the gap between the first `in_bed` start and `Onset`, and only
    then. A Watch-only night has none and says so by omission.
- **`GET /v1/nights/{date}`** in a new `internal/api/nighthandlers.go`, behind
  `requireAuth`. `date` is validated as `YYYY-MM-DD` (422) and a night with no
  resolved intervals is a 404: that night was not recorded, which is a different
  answer from an empty one.
- **The payload's contract** goes in `web/src/lib/types.ts` as `NightDetail` and
  into `contract_test.go`'s list, like every other payload the SPA reads.
- **Tests** in `internal/query/night_test.go` and
  `internal/api/nighthandlers_test.go`:
  - a staged night: intervals in order, onset and wake at its edges, awake
    counted only between them;
  - an in-bed-only night: intervals kept, `Asleep` from the fallback, onset and
    efficiency absent, and nothing zeroed;
  - a night with both: in-bed rows dropped from the stack exactly as the Metric
    drops them, latency present, efficiency on the time-in-bed basis;
  - the figures agree with the Metric: the same night read through `/v1/series`
    and through this endpoint reports the same `asleep`, asserted in one test so
    the two paths are pinned to each other;
  - two Sources on one night: the elected one is reported and the loser's
    intervals are absent;
  - an unrecorded date is 404, a malformed one 422, another Account's night 404.

## Why here

The endpoint is `/v1/nights/{date}` and not `/v1/series?metric=sleep&day=...`
because the second is the loophole: once a Series can be asked for one day at a
finer grain, every caller that has a Series can, and ADR 0012 is enforced by
nothing but discipline. Addressing the entity is what makes the boundary
structural.

The figures live here rather than in the Catalog because none of them folds. A
mean onset over a month averages clock times across dates; an efficiency over a
week is a ratio of sums wearing a rate's unit. The Ledger, the Panel and the CSV
would each have to answer what those mean, and the honest answer is that they
do not.

`EfficiencyBasis` is the same move the Plan page makes with its expenditure
basis (ADR 0023): a figure whose denominator changed with the evidence must name
the evidence, or two nights' percentages are quietly incomparable.

## Comments

Shipped. `internal/query/night.go`, `resolveNight` extracted from
`resolveNights` in `sleep.go`, `internal/api/nighthandlers.go` with its route,
the `NightDetail`/`NightInterval` types in `types.ts` pinned by the contract
test, ADR 0041, the amended **Night** entry in CONTEXT.md, and the pointers on
ADR 0012 and ADR 0027. Tests: `night_test.go` in both packages.

Three things came out of the implementation that the spec did not anticipate:

- **Latency is dropped, not deferred.** The spec had it as a fifth figure,
  computed from an `in_bed` interval preceding the first asleep one. Sharing the
  Metric's resolution makes it uncomputable by construction: in-bed rows are
  dropped whenever real Stages exist, so a night with an onset never has one in
  its kept set, and a night without an onset has no latency to measure. A field
  that can never be populated is worse than an absent feature, so the ADR
  records it as refused rather than leaving a promise in the payload.
- **`efficiency_basis` therefore has one value today.** It stays a field rather
  than becoming a constant, so the day a time-in-bed basis exists the number's
  meaning changes in the payload rather than silently on screen. The same reason
  the Plan names its expenditure basis (ADR 0023).
- **The PRD's new term, Trace, was not created.** CONTEXT.md's **Route** entry
  already lists Trace under `_Avoid_` ("fine in prose, not as the term"), and
  coining it anyway would have contradicted an entry written for a good reason.
  The concept is carried by ADR 0041 and by the entity entries themselves, which
  is where a reader meets it.

One test earns its place beyond the spec's list: the same night read through
`/v1/series` and through `/v1/nights/{date}` must report the same minutes. It is
the assertion that fails the day somebody re-implements the resolution.
