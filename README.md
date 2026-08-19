# Verve

Self-hosted health data warehouse. Verve imports your health data, stores it in
a model that does not depend on the app it came from, and turns it into
dashboards you build yourself.

[![CI](https://github.com/gauthier-se/verve/actions/workflows/ci.yml/badge.svg)](https://github.com/gauthier-se/verve/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

Apple Health is fine for looking at one metric. It is poor at crossing metrics,
comparing periods, and above all at letting you keep what it holds. Verve runs
on your own server: one binary, one database file, no account on anyone else's
infrastructure, no telemetry.

> **Status: pre-release.** Verve is usable day to day and every feature listed
> below is implemented and tested, but no version has been tagged yet, so
> install today means Docker Compose or a build from source. See
> [ROADMAP.md](./ROADMAP.md).

## Features

### Own your data

* **Apple Health import**, from the browser or the command line. Drop the
  `export.zip` the Health app produces and watch the progress bar; nothing to
  unzip, no shell needed.
* **Idempotent re-import.** Re-importing a full snapshot adds only what is new,
  so you can drop in a fresh export every month.
* **107 canonical metrics**, each with a fixed unit and a fixed aggregation
  rule: energy, body composition, activity, heart, respiratory, audio exposure,
  and the full nutrition panel down to individual micronutrients.
* **Nothing is discarded.** Every source is kept side by side. When the Watch
  and the iPhone both counted your steps, Verve resolves the overlap when
  reading rather than deleting rows, and anything it cannot map lands in an
  inspectable bin instead of being dropped.
* **Backup is copying a folder.** One directory holds the database and the
  large files. No dump step, no external database.

### Look at it properly

* **Dashboards you arrange.** Several dashboards, each a grid of panels, each
  panel carrying one to four metrics over a shared time range.
* **Cross-metric panels.** Put sleep against resting heart rate, or intake
  against body mass, on one panel with two axes. Curves keep their real
  magnitude, they are never normalized to look comparable.
* **A headline figure on every panel.** The curve shows the shape, the summary
  shows the size: a true count weighted mean, a total, or a last value,
  computed over the whole window.
* **Period comparison.** Overlay the previous window, the same period last
  year, or a frozen custom window, on every panel at once, with a neutral
  delta. Verve never colors a change green or red: it does not know which
  direction is good for your metric.
* **Derived metrics.** Total expenditure, calorie balance, protein per kilo,
  and the three macro energy shares are computed per bucket from their
  operands.
* **A page per metric**, reachable from any panel title or legend entry, and a
  **Pinned** sidebar for the handful you check daily.
* **The numbers behind the curves.** The ledger shows the same aggregated
  series as a sortable table, so a value can be read exactly and copied out.

### Fill the gaps and plan ahead

* **Manual entry.** Type a measurement the import never captured. It overlays
  the imported data for that day instead of competing with a year of device
  readings, and it stays deletable, which imported data is not.
* **Energy planning.** Verve estimates your resting expenditure from the
  equation of your choice (Katch-McArdle, Cunningham, Mifflin-St Jeor,
  Harris-Benedict) and your total expenditure from the best evidence available,
  and it always names which evidence it used: back-computed from your intake
  and body-mass trend, taken from what your devices recorded, or predicted from
  an equation.
* **Phases.** Commit to a target rate over a stretch of time, cut, bulk or
  maintenance, and see your adherence against it. Phases are kept as history,
  never overwritten, so the past stays answerable.

### Make it yours

* **Appearance.** Light, dark or follow the system, times nine palettes:
  Verve, Catppuccin, Dracula, GitHub, Gruvbox, Nord, Rosé Pine, Solarized,
  Tokyo Night. Each ships both variants, and each is checked for contrast and
  for chart-curve separation rather than copied blindly from upstream.
* **Multi-user with strict isolation.** Accounts never see each other's data.
  The first person to open a fresh instance creates the admin account from the
  browser, after which web signup closes and further accounts are created from
  the CLI.
* **English interface**, one binary, no runtime dependencies.

### What Verve does not do

Verve is not a medical device and gives no diagnosis or medical advice. It does
not phone home, does not sync to a cloud, and has no hosted version. Sleep and
workouts are already imported and stored, but they are not yet visualized, and
ECG waveforms are kept as files without a viewer. See
[ROADMAP.md](./ROADMAP.md).

## Getting started

Verve is one static binary serving both the API and the web UI. All state lives
in a single directory (`VERVE_DATA_DIR`). Migrations apply themselves on
startup.

### Docker Compose, recommended for a homelab

```sh
git clone https://github.com/gauthier-se/verve.git
cd verve
docker compose up -d --build
```

Open <http://localhost:8080>, create your account on the first screen, then
import your export from the Import page. On the iPhone: Health app, profile
picture, Export All Health Data.

The image is [distroless](https://github.com/GoogleContainerTools/distroless)
and runs as a non-root user. The bundled `compose.yml` relaxes the session
cookie for plain-HTTP LAN access; behind an HTTPS reverse proxy, drop the
`--secure-cookie=false` flag.

### From source

Requires Go and Node.

```sh
make dist                                   # builds the SPA into the binary
VERVE_DATA_DIR=/srv/verve ./bin/verve serve --addr=:8080
```

### Importing without a browser

The export can also be loaded from the shell, which is the only way to import
an already unzipped folder or a bare `export.xml`:

```sh
verve -data-dir=/srv/verve import --account=you@example.com export.zip
```

Full configuration, flags, first-run and backup notes:
[docs/deployment.md](./docs/deployment.md).

## Roadmap

What is shipped, what is next, and what Verve will not do:
[ROADMAP.md](./ROADMAP.md).

## Contributing

Verve wants community **connectors**: a connector is compiled into the binary,
and most of its mapping is declarative data rather than code, so adding a source
is mostly a table. Palettes are equally welcome and equally data.

The development setup, the architecture, the conventions and the review
workflow are in [CONTRIBUTING.md](./CONTRIBUTING.md). The vocabulary the code
and the docs both use is in [CONTEXT.md](./CONTEXT.md), and the reasoning behind
every structural choice is recorded as ADRs in [docs/adr/](./docs/adr/).

## Security

Verve holds intimate data. Please report vulnerabilities privately: see
[SECURITY.md](./SECURITY.md).

## License

[Apache-2.0](./LICENSE). Third-party material, including the upstream color
themes the palettes are derived from, is credited in
[THIRD-PARTY-NOTICES.md](./THIRD-PARTY-NOTICES.md).
