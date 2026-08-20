# Sessions are entities, Routes are resources

## Context

Workouts have been imported since the core milestone. Every `<Workout>` lands in
`sessions` with its activity type, its interval, its duration and two totals,
every `<WorkoutRoute>` lands in `routes` with its GPX copied into `artifacts/`,
and nothing has ever read any of it: no endpoint selects from either table, no
endpoint serves an artifact, no screen mentions a workout. It is the same gap
sleep was in before ADR 0027, and it is now the largest one left.

The previous milestone answered its version of this question by refusing a
family-shaped design. Sleep became a Catalog Metric served by the existing
series endpoint, and inherited the Time range, the Baseline, Panels, the
summary, Pins, the Ledger without a second implementation of anything. The
obvious move is to do the same again, and it is wrong here for a reason worth
recording, because the next reader will notice the divergence and assume it is
an oversight.

A second question comes with the routes. A Route is a sequence of points with
timestamps and altitudes, which is intra-day data by nature, and ADR 0012
deliberately caps `/v1/series` at day resolution. The hypnogram was deferred for
exactly that reason. Serving a trace looks like the same breach.

A third comes with the statistics. `<WorkoutStatistics>` reports up to four
aggregates per quantity, and the import kept two figures in total: the distance
sum and the active-energy sum. Heart rate, step count and the rest were dropped
on the floor. ADR 0011's bargain is that ingestion is broad *so that* any of it
can be surfaced later over data already stored, with no re-import and no
migration. For this family the bargain was never honoured, and a detail view is
what makes that visible.

## Decision

**A Session is an entity, not a Metric.** It gets its own endpoints and its own
page: a list, a detail, a map. It gets no Panel, no Pin, no Catalog entry, no
Ledger row.

The line is what the question is *about*. Sleep is a quantity that happens to
arrive as intervals, so "how much did I sleep on the 12th" is a question about a
bucket, and a bucket is exactly what a Series serves. A workout has identity:
"where did I run on Sunday", "how long was that ride", "show me the trace" are
questions about *that* workout, and a bucket is the thing that dissolves the
identity they depend on. A Metric can answer none of them, and dressing a
Session as one would trade the questions it can answer for machinery it cannot
use.

Training volume by activity is the part that genuinely is a Metric, is
expressible as the existing `duration_by_state` shape, and is deliberately not
in this milestone: deciding the entity model while distracted by the aggregation
is how both get decided badly.

**A Route is a resource, not a Series.** It is served under the Session that
owns it, as a simplified polyline with its elevation and pace profiles, derived
from the artefact at read time. ADR 0012 is untouched and its cap still holds:
it governs `/v1/series`, the aggregated-bucket contract every Panel and every
comparison is built on. A Route is not a bucketed aggregation of an Account's
measurements, it is one stored file rendered as itself, and no measurement is
read at sub-day resolution to produce it. Heart rate *during* a workout is on
the other side of that line, lives in `measurements`, and stays out.

**A Session's Routes stay separate.** A workout can carry several GPX files, and
concatenating them draws a line between the end of one segment and the start of
the next, which is ground that was never covered.

**`total_distance` is authoritative wherever a number is shown.** It is what the
device measured; the geometry is our own simplified reconstruction of it, and
the two disagree by a little, always. Geometric length serves the profile's axis
and nothing else.

**Every statistic Apple reports is stored**, keyed `(session, metric, stat)`
with `stat` in `sum | average | min | max`, `metric` a canonical Catalog slug,
`value` in that Metric's canonical unit. An average heart rate and a maximum
heart rate are different answers, and collapsing them loses the one people look
at without anyone deciding to lose it.

**Ingestion stays open, display closes.** The activity type remains a slug
derived from Apple's without a table, so ~80 values arrive and none is dropped
(ADR 0011). A curated table maps the known ones to a label, an icon, a group and
a pace-or-speed reading, and an unknown slug falls back to itself (ADR 0002).
The table lives in `internal/catalog`, not in the web client, because the group
is a server-side filter.

**For a Session, idempotent means convergent.** Re-importing an export attaches
the stats and the routes of a workout that was already present, rather than
skipping it whole. Without this, widening the ingestion would be retroactively
useless for every database that already exists.

**The map's tile layer is opt-in.** `VERVE_MAP_TILES` is empty by default: the
trace is drawn on a blank ground and the browser makes no outbound request.

## Considered Options

- **A Session is an entity with its own page (chosen).** Answers the questions a
  workout raises; costs a list, a detail and a pagination that no existing
  mechanic provides.
- **Workouts as a `duration_by_state` Metric, stacked by activity.** Reuses
  everything, exactly as sleep did, and answers none of the four questions
  anyone asks about a workout: it can say "you did 4 h 20 of cycling in March"
  and can never say which rides those were or where they went. Rejected as the
  milestone, kept as a follow-up: the two are complementary, not alternatives.
- **Both at once.** The aggregation is genuinely wanted and genuinely separable;
  bundling it means the entity model gets decided while attention is elsewhere.
  Deferred.
- **A Session appearing on a Panel as a marker.** That is an Annotation, which
  is the next milestone and owns that model. Doing it here would invent a
  parallel one for a single family and have to keep it. Rejected.
- **Intra-session series from `measurements`, clipped to the workout.** Gives
  heart rate over the session, which is the most-wanted missing curve, and it is
  the intra-day read path ADR 0012 refuses, for one family, through a second
  door. Rejected; it needs its own decision, like the hypnogram.
- **Parsing GPX into a `route_points` table at import.** Makes the profile a
  plain query, and turns an already long import into a much longer one to
  precompute something read rarely, while duplicating on disk what the artefact
  already holds. Rejected.
- **Caching the simplified geometry beside the artefact.** Cheaper per view, and
  it puts a derived file next to a content-addressed one, which brings an
  invalidation question and an asterisk on "backup is copying a folder", for
  tens of milliseconds at this volume. Rejected for now, and a purely local
  addition if a profile ever proves slow.
- **Serving the raw GPX and parsing in the browser.** No server work, and it
  ships megabytes to render a line, and puts the XML parse of an untrusted file
  in the client. Rejected as the display path, kept as a download: a server that
  only ever returns its own simplified version does not honour "your data is
  yours".
- **Concatenating a Session's Routes into one polyline.** Simpler payload, one
  line per workout, and it draws ground that was never covered. Rejected.
- **Geometry authoritative over `total_distance`.** Internally consistent with
  the profile axis, and it puts 9.7 km under a trace the watch calls 10.0 km,
  which makes every other figure on the page suspect. Rejected.
- **Resolving duplicate Sessions across Sources by overlap.** The same run
  recorded by a watch and by a third-party app is two rows today. Ranking or
  merging them is guesswork on entities, where a wrong merge destroys a workout
  rather than smoothing a curve, and there is one Connector. Rejected for now,
  recorded as a known limit beside the roadmap's note on merging Sources.
- **Tiles on by default.** Every map looks finished, and a product that promises
  no outbound traffic starts telling a third party where its owner runs.
  Rejected; opt-in, with attribution rendered when configured.
- **A hand-rolled SVG map to avoid a dependency.** The part that would be
  hand-rolled is not the polyline, it is the Mercator projection, the
  fit-bounds, the pan and zoom, and the tile handling the day someone configures
  a URL. Rejected in favour of one rendering path.
- **Flattening the statistics to one value per quantity.** Half the rows, and it
  silently picks between an average and a maximum. Rejected.
- **Leaving ingestion alone, as a pure read-path milestone.** Consistent with
  ADR 0011 in principle and false in fact for this family, and it would need a
  second migration later reasoning about what the first already displayed.
  Rejected.

## Consequences

- The Catalog gains a second closed table beside the Metrics: the Activity
  display set. Ingestion is unaffected by it, and an unknown activity is
  displayed rather than hidden.
- `session_stats` duplicates two figures already held as columns on `sessions`.
  Deliberate: the list sorts and displays from the columns without a join per
  row, and a promoted column keeps its own unit, since swimming distance is
  canonically metres while `total_distance` is km.
- Re-import gets slightly more expensive for workouts, since stats are written
  on every pass rather than only on first sight. Sessions are hundreds per
  export, so this is not measurable.
- A workout recorded by two Sources appears twice, with its Source shown. This
  is the first place ADR 0003's "resolve at read" has no answer, because it
  resolves per Metric per range and an entity is neither.
- The Workouts page is the second screen with its own Time range, after the
  Metric page, and the first with filters of its own. The Dashboard range does
  not reach it.
- The map is the first part of Verve that can be configured into making an
  outbound request. The default remains no request at all, and a test asserts
  that no tile layer is mounted without configuration, so the promise cannot
  regress silently.
- Heart rate during a workout is now conspicuously absent while its average and
  maximum are on screen. That is the intended shape of the gap: the summary
  figures carry most of the value, and the curve waits for a decision about
  intra-day reads that also settles the hypnogram.
