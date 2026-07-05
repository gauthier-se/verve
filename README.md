# Verve

**Self-hosted, open-source health data warehouse.** Verve ingests your health
data from external sources (Apple Health first), stores it in a canonical model
that does **not** depend on any single source, and visualizes it as customizable
metric dashboards. Your data outlives — and never depends on — Apple Health.

> ⚠️ **Status: early design / pre-alpha.** The architecture and domain model are
> settled (see [`CONTEXT.md`](./CONTEXT.md) and [`docs/adr/`](./docs/adr/)).
> Implementation of the v1 core is just beginning — see
> [`.scratch/v1-socle/`](./.scratch/v1-socle/).

## Why

Apple Health is fine for viewing one metric. It's poor at crossing metrics,
comparing periods, and — above all — **owning your data**. Verve is built for a
homelab: one binary, one database file, your server, your data.

## Key ideas

- **Source-independent canonical model.** Apple Health is just one *Connector*.
  Every reading is normalized into a small set of families — **Measurement**,
  **State**, **Event**, **Session** — against a curated **Catalog** of canonical
  metrics with fixed units and aggregation rules.
- **Nothing is thrown away.** Every source is kept; overlap (e.g. steps from both
  Watch and iPhone) is resolved at read time by per-metric source priority.
  Unmappable data lands in an inspectable *unmapped bin*.
- **Idempotent import.** Re-importing a full Apple snapshot adds only new data.
- **Multi-user with strict isolation** from day one — health data is intimate.
- **Powerful dashboards.** Build your own dashboards of metric panels; roadmap
  adds period comparison, cross-metric overlays, annotations, and derived
  metrics.

## Stack

- **Backend:** Go, single binary. `net/http` stdlib, `log/slog`.
- **Storage:** SQLite (pure-Go `modernc.org/sqlite`), embedded auto-migrations.
  Large artifacts (GPX routes, ECG) stored as referenced files.
- **API:** JSON, returns server-aggregated buckets (never raw series).
- **Front-end:** Vite + React SPA embedded via `go:embed` — TanStack
  Router/Query/Table, shadcn/ui, Recharts.
- **Auth:** local argon2id + sessions (extensible to reverse-proxy forward-auth).
- **Deploy:** distroless Docker image + static binaries; one `VERVE_DATA_DIR`.

The decisions behind these choices live as ADRs in
[`docs/adr/`](./docs/adr/); the ubiquitous language is in
[`CONTEXT.md`](./CONTEXT.md).

## Roadmap

| Milestone | Scope |
|---|---|
| **v1** | Apple import (CLI), Catalog, single-metric dashboards, multi-user |
| **v1.1** | Period comparison (this week vs last, year-over-year) |
| **v1.2** | Cross-metric overlays (sleep vs resting HR, nutrition vs weight) |
| **v1.3** | Timeline annotations |
| **v1.x** | Web self-service import, rich workout UI (GPS map) |
| **v2** | Derived metrics (formula engine) |

## Contributing

Verve wants community **Connectors**. A Connector is compiled into the binary
(interface + registry) with most of its mapping expressed as declarative data —
you contribute one via pull request. See `docs/adr/0009` and `good_practices.md`.

Commits follow [Conventional Commits](https://www.conventionalcommits.org/) and
`main` is protected — see [`CONTRIBUTING.md`](./CONTRIBUTING.md).

## License

[Apache-2.0](./LICENSE).
