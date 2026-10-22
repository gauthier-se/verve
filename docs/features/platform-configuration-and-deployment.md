# Configuration and deployment

- **Status:** Planned
- **Milestone:** M0
- **Related:** [Single binary](platform-single-binary.md), [Backups](backups.md), [Observability](platform-observability.md)

## Summary

Verve is configured through `VERVE_*` environment variables, an optional YAML file and, for some settings, the admin UI. It is distributed as a multi-arch Docker image and as standalone binaries, and is meant to run behind a reverse proxy that terminates TLS.

## User stories

- As a self-hoster, I want a `docker-compose.yml` I can copy and start in minutes.
- As a Raspberry Pi user, I want an arm64 image.
- As an operator, I want environment variables to win over everything else so my infrastructure-as-code stays the source of truth.

## Scope

### In scope

- Configuration sources and precedence.
- Data directory layout.
- Docker image, binaries, example compose file, reverse proxy examples.
- Upgrade behavior.

### Out of scope

- Built-in TLS and ACME.
- Helm chart (future).

## Functional requirements

- **FR-1**: Configuration precedence, highest first: environment variables, YAML file (`--config` flag or `VERVE_CONFIG`), settings stored in the database by the admin UI, built-in defaults.
- **FR-2**: When a setting is defined by environment or file, the admin UI shows it as read-only with the label "Set by environment".
- **FR-3**: Core settings:

  | Variable | Default | Purpose |
  |---|---|---|
  | `VERVE_DATA_DIR` | `/data` | Database, uploads, exports, backups, map files |
  | `VERVE_LISTEN_ADDR` | `:8080` | HTTP listen address |
  | `VERVE_PUBLIC_URL` | none | External URL, used for cookies, links, OIDC redirect and CSRF origin checks |
  | `VERVE_TRUSTED_PROXIES` | none | CIDRs whose `X-Forwarded-*` headers are trusted |
  | `VERVE_SECRET_KEY` | generated | Key for encrypting secrets at rest, generated into `secret.key` if absent |
  | `VERVE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
  | `VERVE_LOG_FORMAT` | `text` | `text` or `json` |
  | `VERVE_MAX_UPLOAD_SIZE` | `10GB` | Upper bound for XML uploads |
  | `VERVE_MAX_INGEST_BODY` | `1GB` | Upper bound for push ingestion bodies |
  | `VERVE_METRICS_ADDR` | none | Optional separate listener for `/metrics` |

  Feature-specific settings (OIDC, SMTP, map, backups, proxy header) are documented in their feature files.

- **FR-4**: Data directory layout:

  ```
  /data
    verve.db, verve.db-wal, verve.db-shm
    secret.key
    uploads/      in-progress resumable uploads
    inbox/        raw push payloads waiting for processing
    exports/      generated user exports
    backups/      database backups
    map/          optional PMTiles and assets
  ```

- **FR-5**: At startup Verve validates the configuration and refuses to start with a clear message on invalid values (for example a malformed CIDR or an unreachable data directory).
- **FR-6**: Without `VERVE_PUBLIC_URL`, Verve starts but shows an admin warning, because secure cookies, OIDC and absolute links depend on it.
- **FR-7**: Docker image:
  - published to GitHub Container Registry for `linux/amd64` and `linux/arm64`,
  - based on `distroless/static` (non-root user), with `/data` as a volume and port 8080 exposed,
  - a `HEALTHCHECK` using `verve healthcheck`,
  - tags: `latest`, semver (`1`, `1.2`, `1.2.3`) and `edge` for the main branch.
- **FR-8**: Binaries for Linux (amd64, arm64), macOS (amd64, arm64) and Windows (amd64), with checksums and signatures, attached to GitHub releases by goreleaser.
- **FR-9**: The repository contains a minimal `docker-compose.yml` and documented reverse proxy examples for Caddy, Traefik and Nginx Proxy Manager, including the large body size needed for uploads and ingestion.
- **FR-10**: Upgrades: schema migrations run automatically at startup, preceded by an automatic backup. Downgrades are not supported. Restoring the pre-migration backup with the previous version is the rollback path.

## UX notes

- Admin settings page groups database-backed settings by area and marks environment-locked ones.

## Data and API notes

- `instance_settings` stores admin-managed values. Secrets in this table are encrypted with the secret key.

## Edge cases

- Losing `secret.key` makes encrypted secrets unreadable: Verve detects it, disables the affected integrations and asks the admin to re-enter them. Health data is not encrypted with this key and stays readable.
- Read-only filesystem or full disk: startup fails with a clear message, and runtime write failures surface on `/readyz`.
- Reverse proxy with a low body size limit breaks uploads: the documentation and the upload error message point to it.

## Acceptance criteria

- [ ] The example compose file starts a working instance on amd64 and arm64.
- [ ] Every configuration key can be set through an environment variable.
- [ ] Environment-defined settings cannot be changed from the UI.
- [ ] An upgrade with a migration creates a backup before migrating.

## Future ideas

- Helm chart.
- Optional built-in TLS for users without a reverse proxy.
