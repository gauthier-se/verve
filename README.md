# Verve

Verve is an open source, self-hostable health dashboard for Apple Health data. It ships as a single Go binary with an embedded web app and a SQLite database, designed to run in a homelab next to the rest of your self-hosted services.

> Status: planning. No code yet. This repository currently contains the product and architecture documentation.

## Why

Apple Health collects years of valuable data (activity, heart, sleep, body measurements, nutrition, workouts), but it stays locked on the iPhone. Verve brings that data to a server you own, keeps every sample, and turns it into dashboards, trends and goal tracking, without any third-party cloud.

## Highlights

- **Your data, your server**: single binary, single SQLite file, no telemetry, AGPL-3.0.
- **Two ways in**: import the full Apple Health export (`export.zip`) for history, then keep it fresh with continuous pushes from the [Health Auto Export](https://apps.apple.com/app/id1115567069) iOS app. A native iOS companion app is planned for later.
- **Nothing is lost**: every quantity and category sample is stored, even when no dashboard uses it yet.
- **Correct numbers**: idempotent imports, no double counting between iPhone and Apple Watch thanks to per-metric source priority, and block rules to keep bad data out for good.
- **Dashboards**: Today overview, a generic metric explorer, sleep, workouts with maps, heart, body and fully customizable dashboards.
- **Goals**: cut, bulk, maintain or recomp plans, with adaptive maintenance calorie estimation from intake and weight trend, and weekly check-ins.
- **Household ready**: multiple accounts with isolated data, local accounts with TOTP, OIDC and trusted proxy header authentication.
- **Homelab friendly**: notifications through ntfy, webhooks or email, scoped API tokens, an OpenAPI-documented REST API, Prometheus metrics, built-in backups.

## How it works

```
 iPhone                                  Homelab
+---------------------------+          +---------------------------------------------+
| Health app                |  upload  |  Reverse proxy (TLS)                        |
|   export.zip (history) ---+--------->|    |                                        |
|                           |          |    v                                        |
| Health Auto Export        |  POST    |  verve (single binary)                      |
|   scheduled JSON push ----+--------->|    - REST API (OpenAPI 3.1)                 |
+---------------------------+          |    - embedded web app (SPA)                 |
                                       |    - ingestion pipeline and job worker      |
 Browser / installed PWA  <----------->|    - SQLite database in /data               |
                                       +---------------------------------------------+
```

## Tech stack

- **Backend**: Go 1.27, `net/http` with [Huma](https://huma.rocks) for a code-first OpenAPI 3.1 API, SQLite through the pure Go `modernc.org/sqlite` driver, sqlc, goose migrations.
- **Frontend**: React single page app built with Vite, TanStack Router, Query, Table, Form and Charts, Tailwind CSS and shadcn/ui, MapLibre GL. Embedded in the binary at build time.
- **Delivery**: multi-arch Docker image (amd64, arm64) and standalone binaries built with goreleaser.

## Development

The toolchain is pinned with a Nix flake. With [Nix](https://nixos.org) and [direnv](https://direnv.net) (plus [nix-direnv](https://github.com/nix-community/nix-direnv) for caching), run once:

```bash
direnv allow
```

Entering the repository then provides Go 1.27, Node.js 26, pnpm 12, golangci-lint 2.15, gopls and delve. Without direnv, use `nix develop`. Go tools (Task, air, sqlc, govulncheck) are pinned in `go.mod` and run through `go tool`; the devshell exposes Task as `task`.

Without Nix, install Go 1.27, Node.js 26, pnpm 12 and golangci-lint 2.15 yourself, and use `go tool task` instead of `task`.

Common tasks (`task --list` shows them all):

| Task | Purpose |
|---|---|
| `task lint` | Go and web linters, format checks and the web type check |
| `task fmt` | Format the Go code |
| `task test` | Go tests with the race detector and web tests |
| `task build` | Build `bin/verve` with version metadata |
| `task web:build` | Build the web app into `web/dist` |

`task go:lint` and `task go:test` run the Go side only, and the `web:*` tasks the web side only, for example `task web:test`. The web app lives in `web/` and also has its own scripts (`pnpm dev`, `pnpm test`, `pnpm lint`) for use inside that directory.

## Documentation

- [Roadmap](docs/roadmap.md): milestones and the list of features.
- [Architecture](docs/architecture.md): components, data flows, data model and cross-cutting rules.
- [Decision records](docs/decisions/): why the main technical choices were made.
- [Features](docs/features/): one specification per feature, built from a [common template](docs/features/_template.md).
- [Contributing](CONTRIBUTING.md): commit conventions (Conventional Commits).

## Disclaimer

Verve is not a medical device and does not provide medical advice. Estimates such as maintenance calories or heart rate zones are approximations meant for personal tracking. Talk to a health professional before acting on health decisions.

## License

[AGPL-3.0](https://www.gnu.org/licenses/agpl-3.0.html). See [decision 0006](docs/decisions/0006-agpl-license.md).
