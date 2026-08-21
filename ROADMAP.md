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
| Sleep | The imported sleep stages read at last: one Metric, bucketed by Night rather than by calendar day, rendered as a stack |
| Workouts | Every statistic Apple reports kept at import, a filterable list carrying its own range, a detail view, and the GPX trace as a map with elevation and pace profiles |
| Annotations | A dated note on the time axis, written once and read against every curve: markers and bands on every panel, a list on the Data page |
| Releases | A tag builds the static binaries and publishes the image, CI compiles the front end on every pull request, and installing is a `docker pull` |
| Cross-metric | Every pinned metric paired against every other over one window, ranked, with a lag — strength and direction, never a cause |
| History | The whole span in one band, phases behind it and gaps drawn as gaps, over a ledger of every import, note, phase and source |

## Next

Verve is on `0.x`. The leading zero is about the API and the interface, which
are still moving, and not about the data, which a tag already protects: see
[ADR 0029](./docs/adr/0029-a-0x-tag-promises-the-data-not-the-interface.md).
Features land before `1.0`, and the criterion for dropping the zero is a second
connector having pushed on the Catalog without breaking it, not a feature count.

Cross-metric and History have landed, which closes the reading side of the
promise: what you have, what it does over years, and what moves with what.
Nothing is committed in their place yet. What comes next is a choice among the
entries below, and the one that moves the leading zero is a second connector.

## Later

* **ECG.** Waveforms are high-frequency recordings that fit none of the current
  families. The files are already referenced and kept; a `Recording` family and
  a viewer come together, or not at all.
* **Meals.** The link between the nutrients logged together is preserved at
  import. Surfacing it answers "what did I eat", which is a different question
  from "how much protein did I get".
* **A hypnogram, and the questions around a night.** Durations per Night are on
  screen; the shape of a single night — stages against the clock — needs an
  intra-day axis the API deliberately does not serve. Sleep onset, wake time and
  efficiency belong with it, and none of them is expressible as a derived
  Metric's Formula.
* **Training volume.** A workout is an entity and gets no Panel (ADR 0028).
  Time or distance per activity, stacked on a time axis, is the aggregate side
  of the same data, and it is expressible as the `duration_by_state` shape
  sleep already uses. It is a Metric question, not a Session one.
* **Intra-workout series.** Heart rate during a ride lives in `measurements`,
  and reading it per workout is the sub-day read ADR 0012 refuses. It belongs
  with the hypnogram above: one intra-day axis, or neither.
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
