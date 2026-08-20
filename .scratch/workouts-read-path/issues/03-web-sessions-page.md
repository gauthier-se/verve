Status: done

# 03: web: the Workouts page, the detail, and a map that phones home only if asked

## What

- **A sidebar entry, "Workouts"**, in `app-shell.tsx` beside the Dashboards and
  the existing pages. The domain says Session and the API says `/v1/sessions`;
  the interface says Workouts, which is the word its owner uses (PRD, and the
  CONTEXT.md note from issue 01).

- **`web/src/components/sessions-page.tsx`**, the list. Its own date range
  control (`RANGE_PRESETS`, as `metric-page.tsx` does) and its own Activity
  group filter, both independent of the active Dashboard's range: this page is
  browsed by "what did I do", not through a window chosen for a curve.
  - A row: date, Activity icon and label, duration, distance, energy, Source,
    and a mark when a trace exists. **No map thumbnail** — one per row is one
    GPX parse per row, an entire screen of them for a glance.
  - Infinite scroll on the API cursor via `useInfiniteQuery`, matching the
    existing `@tanstack/react-query` usage in `web/src/hooks/`.
  - A header carrying the totals **and naming its period and its count**: "12
    workouts, 1 Jan to 31 Mar". A total without its domain reads as a truth and
    is not one, which is the same reason the sleep milestone put `Nights` on the
    wire.
  - The empty state points at `/import`, like `data-page.tsx:174`.

- **`web/src/lib/activities.ts`**, the client half of the display table: slug →
  `lucide-react` icon. The label, the group and the pace/speed reading come from
  the API (issue 02) and are not duplicated here — only the icon, which is a
  React component and cannot cross the wire. An unknown slug gets a neutral
  fallback icon, never a blank.

- **`web/src/components/session-detail.tsx`**: the Session's figures, its stats
  grouped by metric (average and maximum heart rate side by side, not one of
  them), and its Routes. Reached from a list row; a URL of its own so it can be
  linked and reloaded.

- **The map.** Leaflet plus its CSS, added to `web/package.json`, mounted in a
  small `route-map.tsx` wrapper:
  - **One polyline per Route**, never a merged one.
  - `fitBounds` over all segments together.
  - The tile layer is added **only** when the server reports a configured
    `VERVE_MAP_TILES` (surface it on an existing config or auth-state payload
    rather than inventing an endpoint). Empty, which is the default, means a
    blank ground and **zero outbound requests from the browser** — the README's
    "does not phone home" is a promise about the running product, and a default
    basemap would quietly break it by revealing where its owner runs.
  - When a tile URL is configured, render its attribution. Not optional, and not
    the kind of thing to add later.
  - The map colours come from the Palette's chart ramp, like every other
    graphic surface (ADR 0024), so a trace belongs to the page around it in all
    nine Palettes.

- **The profiles**: elevation and pace against distance, drawn with the existing
  `recharts` setup rather than a second charting approach. The axis is the
  geometric distance from the profile payload; the **headline distance stays the
  Session's `total_distance`**, and the two will differ slightly. That is
  expected and must not be reconciled by showing the geometric one.

- **Pace.** `internal/units` gains a pace unit and `web/src/lib/format.ts` a
  `formatPace` producing `5:42/km` — `formatDuration(minutes)` cannot
  (`format.ts:25`). Which of pace or speed a workout shows comes from the API's
  `speed` field, not from a client-side guess about the Activity.

- **Tests** in the register of the existing web tests: the header names its
  period; an unknown Activity slug renders a label and an icon rather than a
  blank; a Session with no route renders the detail without a map rather than an
  empty grey box; with no tile URL configured the map mounts no tile layer (the
  assertion that keeps the privacy promise from regressing silently); pace
  formats as `5:42/km` and speed as `km/h` per the API's field.

## Why here

Two things on this page are one careless commit away from being wrong.

The first is the tile layer. Adding a default basemap is a one-line improvement
that makes every map look finished, and it turns a self-hosted product that
promises no outbound traffic into one that tells a third party where its owner
lives. Default-off with an informed opt-in is the whole point, and the test that
asserts no tile layer without configuration is what keeps it true in a year.

The second is the distance. The map shows a trace, the trace has a length, and
displaying that length is the obvious thing to do — and it will disagree with
the figure the watch recorded, because we simplified the geometry ourselves. A
screen reading 9.7 km under a trace Apple calls 10.0 km makes its reader
distrust every other number on the page, including the ones that are right.

## Comments

Five departures from the spec above.

**The map is lazy-loaded.** Leaflet and its stylesheet are ~150 kB that only a
workout with a trace ever needs, and importing them at the top of the detail page
put them in the first paint of every screen in Verve (the main chunk went from
530 kB to 680 kB). `route-map.tsx` default-exports, the detail page pulls it
through `React.lazy`, and the main chunk is back where it was.

**`internal/units` gained no pace unit.** The spec called for one, and it would
have had no caller: pace never crosses the wire. The API sends a speed in km/h,
because that is what a Route's geometry and timestamps produce, and `formatPace`
turns it into `5:42/km` at the point of display. A unit in `internal/units` earns
its place by being converted *to*, and nothing converts to a pace.

**`useMe` was restructured rather than joined by a second hook.** The map
configuration rides on the payload the SPA already loads at boot, so `useMe` and
`useMapConfig` are now two `select`s over one `["me"]` query: same key, same
fetch, no extra request. Login and register prime that cache and then invalidate
it, because the login payload carries the Account and not the instance settings,
and a configured basemap must not stay invisible until the next reload.

**The map config was on the wrong endpoint, and a test now says so.** The first
version attached it to the *login* response instead of `/v1/auth/me`, so a
configured instance served a client that would never see its tiles. Nothing
failed: `make ci` was green, the page rendered, the map just silently drew no
basemap, which is indistinguishable from the intended default.
`TestMapConfigReachesTheClient` asserts both directions, an unconfigured instance
advertising nothing and a configured one sending the URL and its attribution.

**No JS test runner was added.** The repo deliberately has none: the palette and
sleep-stage contracts are Go tests reading the TypeScript as text so they run in
`make ci` with no front-end toolchain, and `internal/web/workouts_test.go`
follows them. It holds the two things that break silently: every Activity group
has an icon and a filter button, and the tile layer is only ever added inside
`if (tiles)` with no URL hardcoded anywhere in the file. Mutation-checked, both
fail when the guard is removed. What that leaves uncovered is the arithmetic
inside `formatPace`, which no Go test can reach.

Verified against the running binary, not only in tests: a seeded export with a
401-point GPX imports, the list, detail, geometry and GPX download all answer,
the group filter and the unknown-group 422 behave, the SPA serves `/workouts` and
`/workouts/1`, and the second import of the same file adds nothing. The smoke run
also showed the geometry disagreeing with the device (3,71 km of trace against a
declared 6,4 km), which is exactly the number the page is forbidden to show.

`make ci` is green.
