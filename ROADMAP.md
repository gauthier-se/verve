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
| Exclusions | A metric, or a stretch of one, refused at every import and purged from what is stored: one rule that deletes and remembers |
| Google Health | A second connector, reading a Google Takeout: the connector contract lifted out of Apple's package, a registry that recognizes an export by its content, and source resolution moved to day grain so one export cannot take the years another recorded |
| Export | A way out: one account as a portable zip any Verve reads back as an import, and the numbers behind any curve as a CSV at the grain on screen |
| Training volume | The hours and kilometres your workouts come to, per bucket and stacked by activity: the aggregate side of the Sessions the list already holds |
| Intra-day | The shape of one night and the curve of one workout, on their own axes: an entity serves what a bucket destroys |

## Next

Verve is on `0.x`. The leading zero is about the API and the interface, which
are still moving, and not about the data, which a tag already protects: see
[ADR 0029](./docs/adr/0029-a-0x-tag-promises-the-data-not-the-interface.md).
Features land before `1.0`, and the criterion for dropping the zero was a second
connector having pushed on the Catalog without breaking it, not a feature count.

That has now happened. Google Health pushed exactly where a second source was
always going to: it reports one undifferentiated distance where Apple splits it by
activity, and a total energy figure that must not be confused with the one Verve
derives. Both became their own Metric rather than a guess written into stored
rows, and the Catalog held. It also found the read path electing one Source for a
whole window, which a second export turns from a simplification into a way to lose
years of a curve: hence day-grain resolution, and hence the criterion being about
a second connector rather than about a count.

Nothing is committed next yet. What comes after is a choice among the entries
below.

## Later

* **ECG.** Waveforms are high-frequency recordings that fit none of the current
  families. The files are already referenced and kept; a `Recording` family and
  a viewer come together, or not at all.
* **Meals.** The link between the nutrients logged together is preserved at
  import. Surfacing it answers "what did I eat", which is a different question
  from "how much protein did I get".
* **Sharing a Dashboard as JSON.** An Archive deliberately holds data and not
  arrangement (ADR 0039). A Dashboard that exports and imports as a file is the
  other half, and the same shape as a contributed palette: declarative data a
  pull request can carry.
* **Merging sources** rather than only ranking them, for the case where two
  devices have complementary coverage instead of overlapping coverage.
* **More connectors.** The connector interface and its declarative mapping now
  exist as code rather than as a plan, so a third source is a package plus one
  line in the registry. Garmin, Withings and the rest are welcome as pull
  requests. **Health Connect** is the interesting one: Android's on-device store
  exports its own database, which is the only path for someone whose history
  lives on a phone and in no account anywhere.
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
