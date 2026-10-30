# Configuration and deployment

- **Status:** Planned
- **Milestone:** M0
- **Related:** [Single binary](platform-single-binary.md), [Backups](backups.md), [Observability](platform-observability.md)

## Summary

Verve is configured through `VERVE_*` environment variables, an optional YAML file and, for some settings, the admin UI. It is distributed as a multi-arch Docker image and as standalone binaries, and is meant to run behind a reverse proxy that terminates TLS.

## User stories

- As a self-hoster, I want a `compose.yaml` I can copy and start in minutes.
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

  | Variable | YAML key | Default | Purpose |
  |---|---|---|---|
  | `VERVE_DATA_DIR` | `data_dir` | `/data` | Database, uploads, exports, backups, map files |
  | `VERVE_LISTEN_ADDR` | `listen_addr` | `:8080` | HTTP listen address |
  | `VERVE_PUBLIC_URL` | `public_url` | none | External URL, used for cookies, links, OIDC redirect, CSRF origin checks and HSTS (sent only for https) |
  | `VERVE_TRUSTED_PROXIES` | `trusted_proxies` | none | CIDRs whose `X-Forwarded-*` headers are trusted |
  | `VERVE_SECRET_KEY` | none | generated | Key for encrypting secrets at rest, generated into `secret.key` if absent. Not implemented yet (M1): the variable is ignored and has no YAML key |
  | `VERVE_LOG_LEVEL` | `log.level` | `info` | `debug`, `info`, `warn`, `error` |
  | `VERVE_LOG_FORMAT` | `log.format` | `text` | `text` or `json` |
  | `VERVE_MAX_UPLOAD_SIZE` | `max_upload_size` | `10GB` | Upper bound for XML uploads |
  | `VERVE_MAX_INGEST_BODY` | `max_ingest_body` | `1GB` | Upper bound for push ingestion bodies |
  | `VERVE_METRICS_ADDR` | `metrics_addr` | none | Optional separate listener for `/metrics` |

  Value formats:

  - The YAML file is given with `--config`, or `VERVE_CONFIG` when the flag is absent. Unknown keys are errors. An environment variable set to an empty string and an empty YAML value (`key:` or `key: ~`) are ignored: the setting keeps its lower-precedence value.
  - `VERVE_PUBLIC_URL`: an absolute `http` or `https` URL without path, query, fragment or user info (hosting under a sub-path is not supported). A trailing `/` is removed.
  - `VERVE_TRUSTED_PROXIES`: comma-separated CIDR prefixes or single addresses (a single address means `/32` or `/128`). In YAML, a list. IPv4-mapped IPv6 addresses (`::ffff:10.0.0.2`) match IPv4 prefixes. Forwarded headers are honored only when the immediate peer is in this list; otherwise the client IP is the peer address, the scheme comes from the TLS state and the host from the `Host` header. Supported headers:
    - `X-Forwarded-For`: read from the right, skipping trusted hops. The first untrusted address is the client IP, so spoofed entries on the left are ignored. When every entry is trusted, the leftmost one is used. An invalid entry met on the way (`unknown`, empty, malformed) makes Verve fall back to the peer address.
    - `X-Forwarded-Proto`: the rightmost value, `http` or `https` (case-insensitive). Other values are ignored.
    - `X-Forwarded-Host`: the rightmost value.
    - The RFC 7239 `Forwarded` header is not supported and is ignored.

    When an untrusted peer sends `X-Forwarded-*` headers, Verve logs a warning suggesting `VERVE_TRUSTED_PROXIES`, at most once per hour.
  - Sizes: a whole number of bytes, optionally followed by a decimal unit (`KB`, `MB`, `GB`, `TB`, powers of 1000) or a binary unit (`KiB`, `MiB`, `GiB`, `TiB`, powers of 1024), case-insensitive. Must be greater than zero.

  `verve config check` validates the configuration and prints the effective values as YAML, each annotated with its source (`# default`, `# file`, `# env`) and secrets masked. On invalid configuration it prints every error and exits 1.

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

  `serve` creates the missing directories with permissions `0750` and `verve.db` with permissions `0600`. When the data directory is not writable, startup fails with `data directory <path> is not writable: <cause>`.

- **FR-5**: At startup Verve validates the configuration and refuses to start with a clear message on invalid values (for example a malformed CIDR or an unreachable data directory).
- **FR-6**: Without `VERVE_PUBLIC_URL`, Verve starts but shows an admin warning, because secure cookies, OIDC and absolute links depend on it. `serve` also logs the configuration warnings once at warn level at startup.
- **FR-7**: Docker image:
  - published to GitHub Container Registry for `linux/amd64` and `linux/arm64`,
  - based on `distroless/static` (non-root user), with `/data` as a volume and port 8080 exposed,
  - runs as UID and GID `65532`: a new named volume is writable as is, a bind mount must be owned by `65532:65532`,
  - a `HEALTHCHECK` using `verve healthcheck`,
  - tags: `latest`, semver (`1`, `1.2`, `1.2.3`), and `edge` and `sha-<7 chars>` for the main branch,
  - a build provenance attestation on every image pushed from the main branch,
  - release images signed by digest with cosign keyless signing.
- **FR-8**: Binaries for Linux (amd64, arm64), macOS (amd64, arm64) and Windows (amd64), with checksums and signatures, attached to GitHub releases by goreleaser. `checksums.txt` is signed with cosign keyless signing and its bundle is attached.
- **FR-9**: The repository contains a minimal `compose.yaml` and documented reverse proxy examples for Caddy, Traefik and Nginx Proxy Manager, including the large body size needed for uploads and ingestion. See the [Docker](../deployment/docker.md), [reverse proxy](../deployment/reverse-proxy.md) and [binary](../deployment/binary.md) guides.
- **FR-10**: Upgrades: schema migrations run automatically at startup, preceded by an automatic backup. The server already listens while they run and answers 503 until they are applied, see [Observability](platform-observability.md). Downgrades are not supported. Restoring the pre-migration backup with the previous version is the rollback path.

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
