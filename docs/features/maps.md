# Maps

- **Status:** Planned
- **Milestone:** M2
- **Related:** [Workouts](page-workouts.md), [Configuration and deployment](platform-configuration-and-deployment.md)

## Summary

Workout routes are displayed with MapLibre GL. The admin chooses the basemap: a public tile provider by default (OpenFreeMap, no API key), self-hosted vector tiles from a PMTiles file served by Verve itself, or no basemap at all. This lets privacy-minded users avoid any request to a third party.

## User stories

- As a user, I want my runs on a real map out of the box.
- As a privacy-minded self-hoster, I want maps without my browser contacting external tile servers.
- As a user, I want route thumbnails in the workouts list.

## Scope

### In scope

- Map rendering, three basemap modes, route styling, thumbnails.

### Out of scope

- Route privacy zones (future, useful with sharing).
- Downloading PMTiles extracts from Verve (documented, done by the admin).

## Functional requirements

- **FR-1**: Basemap modes, configured by the admin (`VERVE_MAP_MODE` or admin UI):

  | Mode | Behavior |
  |---|---|
  | `public` (default) | Style URL `https://tiles.openfreemap.org/styles/liberty`, overridable with `VERVE_MAP_STYLE_URL` |
  | `self-hosted` | Verve serves a PMTiles file from `/data/map/` (or `VERVE_MAP_PMTILES`) with HTTP range requests, plus an embedded style, sprites and Latin glyphs |
  | `none` | Route on a neutral background with a scale bar, no tiles |

- **FR-2**: When the PMTiles file is configured but missing or unreadable, Verve falls back to `none` and shows an admin warning.
- **FR-3**: Content Security Policy allows only the tile host required by the active mode.
- **FR-4**: Route rendering: line colored by pace, heart rate or elevation with a legend, start and end markers, pause gaps shown as dashed segments, fit to bounds on load.
- **FR-5**: List thumbnails are rendered client-side from the simplified polyline on a plain background (no tile requests for lists).
- **FR-6**: The map supports the synchronized marker from workout charts (see [Workouts](page-workouts.md)).
- **FR-7**: Dark theme uses a dark basemap variant when the style provides one.

## UX notes

- Admin map settings page shows the current mode, the PMTiles file name, its size and covered bounds, and a preview map.
- Documentation explains how to extract a region with the `pmtiles` CLI from a Protomaps build.

## Data and API notes

- `GET /map/tiles.pmtiles` (range requests), `GET /map/style.json`, `GET /map/sprites/*`, `GET /map/glyphs/*` in self-hosted mode, authenticated by session.

## Edge cases

- Routes outside the PMTiles extract: route visible on an empty area, with a hint.
- Very long routes (ultra events): simplified on the client according to zoom.
- Offline or blocked public tile host: the route still renders without basemap.

## Acceptance criteria

- [ ] In `self-hosted` and `none` modes, the browser makes no request to any external host when viewing a workout.
- [ ] Switching mode requires no restart.
- [ ] Thumbnails in the workouts list make no tile requests.

## Future ideas

- Route privacy zones around chosen locations.
- Heatmap of all routes.
