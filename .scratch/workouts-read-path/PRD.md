# PRD: Workouts read path

## Goal

Show the workouts Verve already holds. `<Workout>` has been imported since the
core milestone: one `sessions` row per workout with its activity type, its
interval, its duration and two totals, plus one `routes` row per GPX with the
file copied into `artifacts/` (`internal/connector/applehealth/workout.go`,
`routes.go`). Nothing reads any of it. No endpoint selects from `sessions`, no
endpoint serves an artifact, no screen mentions a workout. This is what sleep
was until the previous milestone, and it is now the largest remaining gap
between what a Verve database contains and what its owner can see.

## The concept

**A Session is an entity, not a Metric.** The previous milestone answered its
question by refusing a family-shaped design: sleep became a Catalog entry read
by `GET /v1/series`, and inherited the range, the Baseline, the Panels, the
summary, the Pins, the Ledger without a second implementation. That answer does
not transpose here, and the ADR must say why rather than leave the divergence
to be read as an inconsistency.

Sleep is a quantity that happens to arrive as intervals: "how much did I sleep
on the night of the 12th" is a question about a bucket, and a bucket is what a
Series serves. A workout is an object with identity. "Where did I run on
Sunday", "how long was that ride", "show me the trace" are questions about
*that* workout, not about a bucket containing it. A Metric can answer none of
them, because the bucket is exactly the thing that dissolves the identity.

So Sessions get what a Series cannot give: a list, a detail, a map. What they
deliberately do not get in this milestone is a presence on any Dashboard.
Training volume as a stacked Metric by activity is a real and separate want, it
is expressible as the existing `duration_by_state` shape, and it is a later
milestone. Doing both at once would mean deciding the entity model while
distracted by the aggregation.

**A Route is a resource, not a Series.** ADR 0012 caps `/v1/series` at the day,
and the sleep PRD deferred the hypnogram for exactly that reason. A Route
escapes that cap without touching it: the GPX carries an instant and an
altitude per point, so the trace, the elevation profile and the pace profile
are all derived from one artefact, served under the Session that owns it. No
measurement is read at sub-day resolution, and the series contract is untouched.
Heart rate *during* a workout is the other side of that line: it lives in
`measurements`, reading it per workout is the intra-day read ADR 0012 refuses,
and it stays out.

### Activity

An **Activity** is what a Session was: `running`, `cycling`, `traditional
strength_training`, derived as a neutral slug from Apple's type without a table
(`families.go:47`), so the set is open by construction and holds Apple's ~80
values. Ingestion stays open, per ADR 0011. **Display** closes: a curated table
maps a known slug to a label, an icon, a group and a speed reading, and an
unknown slug falls back to its prettified self. The group (`cardio`, `strength`,
`water`, `winter`, `other`) is the list's filter, so it is server-side data and
not a web-only decoration.

### Session stat

A **Session stat** is one summary figure Apple reports for a workout:
`<WorkoutStatistics>` carries a type and up to four aggregates, `sum`,
`average`, `minimum`, `maximum`. Today `addStatistic` keeps two of them, the
distance sum and the active-energy sum, and drops heart rate, step count, basal
energy and the rest on the floor (`workout.go:93`). That is a live breach of
ADR 0011's promise: capture broadly so anything can be shown retroactively over
data already stored, no re-import, no migration. The promise is already broken
for this family, and a detail view worth opening is what makes it visible.

So ingestion widens, keyed `(session_id, metric, stat)`, `metric` being the
canonical Catalog slug through the mapping that already exists (`typeToMetric`
in `mapping.go`) and the unit conversion the Catalog already declares. No
second vocabulary for the same quantities. `total_distance` and `total_energy`
stay as columns, promoted stats: they are the list's sort and display keys, and
a join per row to read a distance would be paid on every screen.

Widening ingestion means the historical import has to converge, not just be
skipped. It does not today: `InsertSession` is `INSERT OR IGNORE` and, on a
workout already present, only recovers its id (`internal/data/session.go:60`).
Re-dropping the same zip after this milestone would therefore leave every old
workout stat-less forever. Idempotent, for a Session, must mean **convergent**,
not inert.

## What this milestone does

- **`session_stats`**, a new table keyed `(session_id, metric, stat)` holding
  every aggregate Apple reports, in Catalog units. `addStatistic` stops
  switching on two types and records them all; the two promoted columns keep
  being written as they are today.

- **A convergent re-import.** When `InsertSession` finds the workout already
  present, its stats and its routes are still attached. A second import of the
  same export becomes a no-op only when there is genuinely nothing new, which
  is what the README already promises re-import does.

- **`GET /v1/sessions`**, the list: filtered by date range, by Activity group
  and by Activity, paginated by cursor on `start_at` descending, each row
  carrying date, activity, duration, distance, energy, Source, and whether it
  has a route. Plus the totals over the active filter, and the count of Sessions
  they were computed from.

- **`GET /v1/sessions/{id}`**, the detail: the Session, its stats, and its route
  references.

- **`GET /v1/sessions/{id}/routes`**, the geometry: each Route as a simplified
  polyline (Douglas-Peucker) with its elevation and pace profiles, parsed from
  the artefact on demand. Nothing is persisted: the artefact is the source of
  truth (ADR 0004), and a derived file beside a content-addressed one would
  bring an invalidation question and an entry in "backup is copying a folder"
  that earns nothing at this volume.

- **`GET /v1/sessions/{id}/routes/{routeID}.gpx`**, the raw bytes. "Your data is
  yours" does not survive a server that only ever returns its own simplified
  version.

- **A Sessions page**, its own sidebar entry, with its own filters and its own
  date range, independent of the active Dashboard's. A workout list is browsed
  by "what did I do", not through a window chosen for a curve; binding the two
  would mean switching Dashboard to find last year's race. Precedent is already
  split in the tree: the Metric page carries its own range control, the Ledger
  overview carries none, Panels follow the Dashboard.

- **The list's header names its period.** Totals over a filter, with the number
  of Sessions counted, stated. Same rule as the sleep milestone's `Nights`: a
  total without its domain reads as a truth and is not one.

- **The detail view**: the Session's figures, its stats, and its Routes drawn on
  a map, one polyline per Route.

- **N Routes stay N polylines.** A workout can carry several GPX files
  (`workout.go:37` accumulates a slice). Concatenating them draws a straight
  line between the end of one segment and the start of the next, which is a
  stretch of ground you did not cover. Segments stay separate.

- **`total_distance` is authoritative wherever a number is shown.** It is the
  device's measurement; the geometry is our own simplified reconstruction.
  A screen reading 9.7 km under a trace Apple calls 10.0 km makes every other
  figure suspect. Geometric length serves the elevation profile's axis, and
  nothing else.

- **Leaflet, with an optional tile layer.** `VERVE_MAP_TILES` is empty by
  default: the trace is drawn on a blank ground and the browser makes no
  outbound request, so the README's "does not phone home" stays literally true.
  Filling it with a tile URL, OSM or your own server, is the account holder's
  informed choice. One rendering path either way: the part we would otherwise
  hand-roll is not the polyline, it is the Mercator projection, the fit-bounds,
  the pan and zoom, the attribution and the zoom levels.

- **A closed display table for Activities**, mapping a known slug to its label,
  icon, group and speed reading, unknown slugs falling back to the prettified
  slug. Ingestion open, display closed: ADR 0011 and ADR 0002 each keep their
  half.

- **Pace or speed per Activity.** A run reads in min/km, a ride in km/h, a
  strength session in neither. The choice is a column of the display table, not
  an account preference: the data knows the answer. `internal/units` gains a
  pace unit, `format.ts` a formatter for `5:42/km`, which `formatDuration`
  cannot produce.

## What this milestone does NOT do

- **Nothing on a Dashboard or a Panel.** No Metric, no Pin, no Ledger row.
  Training volume as a stacked Metric by Activity is the natural follow-up and
  it is a milestone, not a checkbox at the end of this one.

- **No session marker on a curve.** Marking a workout on a Panel's time axis is
  an Annotation, which is the next roadmap item and owns that model. It is named
  here precisely because it is the thing most likely to sneak in through the
  back door with a rushed data model.

- **No heart rate during a workout.** It needs the intra-day read of
  `measurements` that ADR 0012 refuses, exactly like the hypnogram. The stats
  give the average and the max of the session, which is most of the value at
  none of the cost.

- **No manual workout entry.** Sessions come from a Connector in this milestone.
  Typing a workout needs a writable path into `sessions` and its own Manual
  overlay rule, like manual sleep does.

- **No deletion.** Imported data is not deletable (ADR 0022), and a Session is
  imported. Nothing here changes that.

- **No de-duplication across Sources.** The same run recorded by a Watch and by
  a third-party app yields two `sessions` rows with distinct content keys, and
  both are listed with their Source shown. There is one Connector today, so the
  case is theoretical; a wrong merge on an entity is far worse than a visible
  duplicate on a series. Recorded as a known limit, next to the roadmap's
  existing note on merging Sources, so the first Garmin contributor meets it in
  writing rather than in production.

## Ordering

This milestone comes **after** a tagged release, not before it. The roadmap had
it first; goreleaser is already configured, and the tag, the image and the
release page are the only remaining work that changes *who can run Verve at
all*. Every milestone placed ahead of it delays the first user who is not its
author. The README and ROADMAP updates for workouts therefore land at merge
time, after the tag has shipped.

## Docs

- **ADR 0028**, carrying the two decisions this milestone rests on: a Session is
  an entity with its own page rather than a Catalog Metric, and a Route is
  served as its own resource rather than as a Series. Rejected alternatives to
  record: workouts as a stacked `duration_by_state` Metric by Activity (defers
  the identity questions, answers none of them); tiles on by default; a
  `route_points` table filled at import; geometry authoritative over
  `total_distance`; concatenating a Session's Routes into one polyline;
  whole-window Source ranking or overlap merging for duplicate Sessions.

- **CONTEXT.md entries** for **Activity**, **Session stat** and **Route**, each
  with its `_Avoid_` list, plus a line on the existing **Session** entry
  (`CONTEXT.md:47`) now that the family is read, and the note that the domain
  says Session while the interface says Workouts.

- **README and ROADMAP**: workouts move from "Next" to "Shipped", the "not yet
  visualized" sentence in "What Verve does not do" goes away, and the feature
  list gains the page. At merge time, after the release milestone.

## Issues

1. `01-session-stats-and-convergent-import`, data and connector: the
   `session_stats` table and its migration, the widened `addStatistic`, the
   convergent re-import, the ADR and the CONTEXT.md entries.
2. `02-sessions-across-the-api`, data and api: the list with its filters, cursor
   and totals, the detail, the route geometry and profiles, the raw GPX
   download, and the Activity display table that the group filter needs
   server-side.
3. `03-web-sessions-page`, web: the sidebar entry, the list and its header, the
   detail, the Leaflet map and its optional tile layer, the pace unit and its
   formatter.
