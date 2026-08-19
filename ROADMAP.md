# Roadmap

Verve is built in milestones, each one a small set of issues under
`.scratch/<milestone>/` with a PRD, and each structural decision recorded as an
ADR in [docs/adr/](./docs/adr/). This page is the map: what already works, what
comes next, and what Verve deliberately will not do.

Nothing here carries a date. Verve is a personal project built in the open, so
the order is a statement of priority, not a schedule.

## Shipped

Everything below is merged into `main`, tested, and usable today.

| Milestone | What it gave you |
| --- | --- |
| Core | Apple Health import from the CLI, the canonical catalog, aggregated-bucket API, dashboards of panels, multi-user auth, Docker and binary packaging |
| Derived metrics | Total expenditure, calorie balance, protein per kilo, macro energy shares, computed per bucket from a declarative formula |
| Period comparison | A dashboard-wide baseline window (previous, same period last year, custom) overlaid on every panel |
| Time axis | One module owning bucket boundaries, with the Go and SQL sides pinned to the same contract by test |
| Web onboarding | First-run account creation from the browser, then closed signup; a seeded dashboard so no account starts empty; self-service zip import with real progress |
| Panel summary | A headline figure on every panel, computed server-side as a single bucket over the range |
| Cross-metric overlays | One to four metrics per panel, at most two units, two axes, one summary per series |
| Data ledger | The aggregated series as a sortable table, overview and per-metric detail |
| Manual entry | Typed measurements carrying a reserved Manual source, overlaying imported days, deletable because they are yours |
| Energy planning | Basal and expenditure estimates with a named basis, target rates, phases and adherence, on a Plan page |
| Metric page | A page per metric, linked from every panel title and legend entry |
| Appearance | Light, dark or system, times nine palettes, each verified for contrast and chart separation |
| Pins | Metrics kept in the sidebar as shortcuts, deliberately without a time axis |

## Next

The three items that would change what Verve can answer, in the order they
matter.

### Read path for sleep

Sleep is already imported and stored as **State** rows, one per stage with a
start and an end. Nothing reads them: the query engine only aggregates
measurements, no endpoint serves them, no screen shows them. Closing this
requires an interval-aware aggregation, an endpoint, and a panel type that
renders stages as stacked bars rather than a curve. The data is in your
database right now, which makes this the largest gap between what Verve holds
and what it shows.

### Read path for workouts

Same story for **Sessions**: workouts, their summary statistics, and their GPX
routes are ingested and the route files are kept as artifacts. What is missing
is a list, a detail view, and a map. Deferred from the core milestone on
purpose, still deferred, still wanted.

### Annotations

The fourth differentiator promised alongside period comparison, cross-metric
panels and derived metrics, and the only one not built. A dated note on the
time axis, visible across dashboards, so a curve can be read against what was
happening: an illness, a trip, a change of program. Not yet specified.

### A tagged release

There is no published version and no published image, so installing means
cloning and building. A first tag, a container image on a registry, and the
binaries goreleaser is already configured to produce would turn Verve from
something you build into something you install.

## Later

* **ECG.** Waveforms are high-frequency recordings that fit none of the current
  families. The files are already referenced and kept; a `Recording` family and
  a viewer come together, or not at all.
* **Meals.** The link between the nutrients logged together is preserved at
  import. Surfacing it answers "what did I eat", which is a different question
  from "how much protein did I get".
* **Merging sources** rather than only ranking them, for the case where two
  devices have complementary coverage instead of overlapping coverage.
* **More connectors.** The connector interface and its declarative mapping
  exist precisely so that a second source is a contribution rather than a
  rewrite. Garmin, Withings, Health Connect and the rest are welcome as pull
  requests.
* **Forward-auth SSO.** The auth middleware was kept extensible for it.
* **Invitations**, if closed signup plus CLI account creation turns out to be
  too sharp an edge for households.

## Not planned

* **A hosted version.** Verve is self-hosted. There is no cloud to sync to,
  which is the point.
* **Medical interpretation.** Verve shows what your data says and names the
  evidence behind every estimate. It does not diagnose, does not advise, and
  does not color a change good or bad, because it cannot know which direction
  is good for your metric.
* **Open public signup.** A single binary behind a reverse proxy with a
  standing signup form is a liability, not a feature.
* **A general formula language.** Derived metrics are declarative data on
  purpose: a closed shape that can be validated, not an expression evaluator.
* **Telemetry**, of any kind.
