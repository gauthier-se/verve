Status: done

# 02: query, api: the Measurements inside one workout, bucketed by its own span

## What

- **`internal/query/workoutseries.go`**: the Measurements of one Metric whose
  interval falls inside a Session's window, folded into at most
  `workoutBuckets = 300` buckets spread evenly across that window.
  ```go
  type WorkoutSeries struct {
      Metric  string        `json:"metric"`
      Unit    string        `json:"unit"`
      Source  string        `json:"source"`
      StartAt string        `json:"start_at"` // the window, echoed so a client
      EndAt   string        `json:"end_at"`   // draws an axis without guessing
      Points  []WorkoutPoint `json:"points"`
  }
  type WorkoutPoint struct {
      At    string  `json:"at"`    // the bucket's start, RFC 3339
      Value float64 `json:"value"` // the mean of its readings
      Count int     `json:"count"`
  }
  ```
- **The grain is the workout's, not the caller's.** The bucket is
  `duration ÷ 300`, floored at one second: a 20-minute run and a 6-hour ride
  both come back bounded, and there is no parameter to widen. That is what keeps
  this from becoming a general sub-day read by increments (ADR 0041).
- **An empty bucket is an absent Point**, as everywhere else: a gap is not a
  zero (ADR 0014), and a heart-rate monitor that dropped out for a minute did
  not record a zero.
- **The Source is elected once over the workout** with the existing priority
  table (ADR 0003), not per day and not per bucket: inside one session "which
  device" has one answer, and switching mid-ride would draw a discontinuity that
  is an artefact of the read.
- **The Metric is validated**: it must exist in the Catalog, be `Imported`, and
  have an `average` or `sum` rule. `latest` has no meaning inside an hour, and a
  by-state rule reads another family entirely. Anything else is a 422 naming
  which.
- **`GET /v1/sessions/{id}/series?metric=heart_rate`** in
  `sessionhandlers.go`, beside the routes endpoint it is modelled on. Ownership
  through `sessionOr404` like every other Session read, so another Account's id
  is a 404 (ADR 0007). No samples is an empty `points` array and a 200: the
  workout exists, the curve does not.
- **One Metric per request.** Two units inside a ride is the dual-axis question
  a Panel answers, and a workout page is not a Panel. Asking twice is two
  requests and costs one round trip.
- **Tests** in `internal/query/workoutseries_test.go` and
  `internal/api/sessionhandlers_test.go`:
  - readings inside the window are bucketed and averaged, and the count is the
    readings behind each bucket;
  - a reading one second before the start and one at the end bound are both out,
    since the window is half-open like every other;
  - a long workout yields at most 300 points, a short one fewer, and neither
    takes a parameter;
  - a minute with no reading is an absent Point, not a zero;
  - two Sources in the window: one is elected and reported, and the other's
    readings are not averaged in;
  - `latest`, derived, by-state and unknown Metrics each 422;
  - a workout with no readings of that Metric is 200 with no points;
  - another Account's session id is 404.

## Why here

It hangs off the Session rather than off `/v1/series` for the reason ADR 0041
records: the axis belongs to the entity. The endpoint cannot be pointed at a
range, a Dashboard or a Panel, because it takes no range at all: the window is
the workout's own, echoed back so the client draws an axis without inventing
one.

The 300-bucket cap and the mean are the same trade the Route already makes by
simplifying a polyline server-side (ADR 0028): a screen gets a shape it can draw
and the stored rows stay untouched. It is a mean rather than a sample because a
heart-rate curve's whole content is where it sat, not which instants were
picked.

## Comments

Shipped. `internal/query/workoutseries.go`, `handleSessionSeries` beside the
routes handler, the route, and the `WorkoutSeries`/`WorkoutPoint` types pinned
by the contract test. Tests: `workoutseries_test.go` and four cases in
`sessionhandlers_test.go`.

Three notes:

- **The Source election is its own small function** rather than a reuse of
  `resolveSource`. That one answers a different question, per day and with the
  Manual overlay folded in, and bending it to a single window would have made
  both callers harder to read than the eight lines it replaces. The priority
  table itself is shared, which is the part that must not diverge.
- **The bucket floor is one second.** `span / 300` on a two-minute warm-up is
  400ms, finer than any stored timestamp, so the curve would have been one
  reading per bucket with a misleading number of them.
- **A workout whose stored end is not after its start answers 422**, not 500.
  The row is wrong rather than the request, and the message says there is no
  axis to read along.
