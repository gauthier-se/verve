Status: done

# 02: data, api: the Sessions list, the detail, and the Route as a resource

## What

- **`internal/data/session.go` gains the reads.** Everything so far is writes.
  - `ListSessions(ctx, accountID, filter)` where filter carries `From`, `To`,
    optional Activity slugs, optional Activity groups, a cursor and a limit.
    Ordered `start_at DESC, id DESC`; the cursor is that pair, not an offset, so
    a page cannot shift under a concurrent import. `sessions_account_start`
    already indexes the ordering.
  - `SessionTotals(ctx, accountID, filter)`: count, total duration, total
    distance, total energy over the *same* filter minus the cursor. One query,
    not a fold over a page, because the header describes the filter and not the
    page.
  - `GetSession`, `SessionStats`, `RoutesForSession`, each account-scoped in the
    WHERE clause rather than by trusting the id.

- **The Activity display table**, `internal/catalog/activity.go`. It is Catalog
  data, not a web asset, because the group is a server-side filter:
  `{slug, label, group, speed}` with `group` in `cardio | strength | water |
  winter | other` and `speed` in `pace | speed | none`. Cover the activities a
  person actually records and let the rest fall through: `ActivityDisplay(slug)`
  returns the entry or a derived one (`traditional_strength_training` →
  "Traditional Strength Training", group `other`, speed `none`). Ingestion stays
  open (ADR 0011), display closes (ADR 0002), which is the same split the
  Catalog and the Palettes already use. A test asserts the fallback never
  panics and never returns an empty label.

- **`internal/route`** (new package), the only code that understands GPX:
  - `Parse(r io.Reader) (Track, error)` reading `trkpt` lat/lon/ele/time. Apple
    writes one `trkseg`; tolerate several. Reject nothing for a missing `ele` or
    `time` — a point without elevation drops out of the elevation profile only.
  - `Simplify(track, maxPoints)` — Douglas-Peucker with a tolerance chosen to
    land under `maxPoints` (start at 1000). A 20 000-point ride must not become
    a 20 000-point JSON payload.
  - `Profiles(track)` — cumulative geometric distance (haversine) as the axis,
    elevation against it, and pace/speed against it from consecutive
    timestamps. Smooth the pace over a small window; raw per-point pace from
    consumer GPS is noise, not data.
  - Unit tests on a small fixture: a known-length straight track, a track with
    a missing `ele`, a single-point track, an empty track (all must return
    something renderable or a clean error, never a panic).

- **`GET /v1/sessions`** — `from`, `to`, `activity` (repeatable), `group`
  (repeatable), `cursor`, `limit`. Responds with the page, the next cursor, and
  the totals block carrying `count`, `duration`, `distance`, `energy` **and the
  resolved `from`/`to`**. The client must be able to name the period it is
  showing a total for without re-deriving it; same rule as `Series.Nights`.
  Each row carries `has_route` so the list can mark a trace without loading one.

- **`GET /v1/sessions/{id}`** — the Session, its stats grouped by metric with
  their Catalog units, and its route references. 404 for another Account's id,
  never 403: the id's existence is not this Account's business.

- **`GET /v1/sessions/{id}/routes`** — every Route of the Session, each with its
  simplified polyline and its profiles, parsed from the artefact on demand and
  cached nowhere (PRD: the artefact is the source of truth, ADR 0004). One
  entry per Route: they are **not** concatenated, because the joining segment is
  ground that was never covered.

- **`GET /v1/sessions/{id}/routes/{routeID}.gpx`** — the raw bytes,
  `Content-Disposition: attachment`. Resolve the file from the `artifact` column
  joined to the owning Session, then `filepath.Join(artifactsDir, base)` with
  `filepath.Base` applied — the name is content-addressed and cannot contain a
  separator today, and the guard costs one call.

- **Response shape note**: distance and energy in a Session response are the
  `total_distance`/`total_energy` columns. The geometry's own length is exposed
  only inside the profile payload, as the profile's axis. Nothing else may
  report a distance derived from geometry (PRD: the device's measurement is
  authoritative wherever a number is shown).

- **Handler tests** in the register of `ledgerhandlers_test.go`: the filters
  each narrow, the group filter resolves through the display table, the cursor
  paginates without overlap or gap across a boundary, the totals cover the
  filter and not the page, another Account's Session is a 404 on all four
  routes, a Session with no route returns an empty list rather than a 404, a
  malformed cursor is a 400.

## Why here

The list is where the Session model gets tested against a real question, and
two of its details are the ones that go wrong quietly. The cursor: offset
pagination looks identical in a test and drops rows the moment an import runs
during browsing. The totals: computing them from the returned page is one line
shorter and produces a header that is wrong in a way nobody notices, because it
looks like a total and is a page sum.

`internal/route` is a package rather than a file in `internal/api` because it is
the second thing in the tree that turns a stored artefact into something
displayable, and because parsing untrusted XML deserves a boundary with its own
tests. Keeping the parse on-demand rather than persisted is what keeps "backup
is copying a folder" true, and the volume genuinely does not justify a cache:
one detail view, one parse, tens of milliseconds.

The Activity display table sits in `internal/catalog` and not in `web/` because
the group is a *filter*. If the server does not know the groups, the client has
to send an enumerated list of slugs for "all cardio", which breaks the day Apple
adds an activity and every old client filters it out.

## Comments

Four departures from the spec above, each found while writing it out.

**The listing's window covers the whole of its To day, unlike a Series.** The
range tokens resolve to `[from, midnight-opening-today)` everywhere else, so a
7-day Panel stops at yesterday: the running day is an incomplete bucket and a
half-height bar reads as a bad day. A list has no buckets and no such trap, and
the workout somebody is most likely looking for is the one they finished an hour
ago. `TestListSessionsIncludesToday` stands over it.

**An absent `range_preset` means every workout.** Elsewhere the preset is
required, because a Panel must be told what window to draw. A list of things you
did has an obvious default, and making the page send `range_preset=all` to mean
"no filter" is ceremony.

**Profiles are measured over every recorded point and only their output is
decimated.** The spec had `Simplify` then `Profiles`, which loses exactly what
the elevation profile is for: Douglas-Peucker works on the ground plan, so a hill
climbed on a straight road collapses to its two endpoints and its ascent
disappears with them. `Compute` now takes a sample cap and strides its output;
`TestComputeDecimatesWithoutLosingTheClimb` fails if that is undone. Ascent also
gained a 3 m hysteresis, because summing every positive delta of a consumer GPS
altitude reports a climb over a trace that never moved.

**A Route whose artifact is missing is skipped, not fatal.** A row can outlive
its file only if the data directory was edited by hand, and when it does the
workout's other segments, its stats and its figures are all still true. It logs
and continues rather than turning one absent file into a broken page.

Also worth noting for issue 03: `InsertRoute` does not set the inserted row's
id, unlike `InsertSession`. The web client reads route ids from the payload, so
nothing needs it today; a test that wants one has to read it back.

`make ci` is green.
