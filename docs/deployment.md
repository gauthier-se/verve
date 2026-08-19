# Deploying Verve

Verve is one static binary that serves both the JSON API and the embedded web UI
(ADR 0005). All state lives in a single directory. There is no external database,
no Node runtime in production, and no separate web server.

## Configuration

Verve is configured by one environment variable and a handful of flags. There is
no config file.

### Data directory

| | |
|---|---|
| **Env** | `VERVE_DATA_DIR` |
| **Flag** | `-data-dir=DIR` (takes precedence over the env var) |
| **Default** | `./data` |

The data directory holds everything Verve owns:

```
$VERVE_DATA_DIR/
  verve.db        SQLite database (schema + all imported readings)
  verve.db-wal    write-ahead log     ┐ transient SQLite sidecar files
  verve.db-shm    shared-memory index ┘
  artifacts/      large referenced files (GPX workout routes, ECG), ADR 0004
```

It is created on first run if missing, and database migrations auto-apply on
every startup, so there is no manual migrate step for the operator.

The global flag must precede the subcommand, because flag parsing stops at the
first non-flag argument:

```sh
verve -data-dir=/srv/verve account create --email=you@example.com   # correct
verve account create --data-dir=/srv/verve --email=you@example.com  # wrong
```

### Server flags (`verve serve`)

| Flag | Default | Purpose |
|---|---|---|
| `--addr` | `:8080` | address the HTTP server listens on |
| `--secure-cookie` | `true` | set the `Secure` attribute on the session cookie; **disable only for plain-HTTP local/LAN access** |
| `--max-upload-mb` | `0` | reject a web import upload larger than this many MiB; `0` uses the built-in default of 2 GiB |

Behind an HTTPS reverse proxy, keep `--secure-cookie=true` (the default) so the
session cookie is only ever sent over TLS. On a plain-HTTP LAN, set
`--secure-cookie=false` or logins won't stick.

### Other commands

| Command | What it does |
|---|---|
| `verve version` | print the build version |
| `verve migrate` | apply migrations explicitly (idempotent; also runs on every startup) |
| `verve account create --email=EMAIL` | create an account (prompts for a password) |
| `verve account passwd --email=EMAIL` | set an account's password |
| `verve import --account=EMAIL FILE` | import an Apple Health export (`.zip` or `export.xml`) |

Password commands prompt interactively; pass `--password-stdin` to read the
password from standard input instead (for scripting).

## First run

Verve is multi-user with strict isolation (ADR 0007). There is no default
account, and web signup is open only while the instance holds **zero** accounts:
the first person creates the admin account from the browser, and signup then
closes, enforced server-side (ADR 0017). Further accounts are created from the
CLI.

```sh
# 1. Point at a data directory (or rely on the ./data default).
export VERVE_DATA_DIR=/srv/verve

# 2. Start the server, then open http://localhost:8080
verve serve --addr=:8080
```

The first screen asks for an email and a password, opens the session
immediately, and lands you on a seeded dashboard (ADR 0018). From there the
Import page accepts the `export.zip` the iPhone Health app produces (profile
picture, Export All Health Data): the upload streams to the data directory and
the import runs as a background job with real progress (ADR 0016).

### The CLI path

Everything above can also be done from a shell, which is the only way to create
accounts beyond the first, and the only way to import an already unzipped
folder or a bare `export.xml`:

```sh
verve account create --email=you@example.com          # prompts for a password
verve import --account=you@example.com export.zip
```

Re-importing the same export is idempotent: only new readings are added
(ADR 0006), so the import can be re-run after every new snapshot.

## Backup

Because everything lives in one directory, **backup is copying the data dir**:
no `pg_dump`, no export step.

```sh
# Stop the server (or accept a crash-consistent copy) and copy the directory.
cp -a /srv/verve /backups/verve-$(date +%F)
```

SQLite in WAL mode keeps `verve.db-wal`/`verve.db-shm` alongside `verve.db`. For
a guaranteed-consistent snapshot while the server runs, prefer SQLite's own
backup rather than a raw file copy:

```sh
sqlite3 /srv/verve/verve.db ".backup '/backups/verve.db'"
# and copy the artifacts/ directory alongside it.
```

Restore by stopping Verve and putting the directory (or the `.db` file plus
`artifacts/`) back in place; migrations reconcile the schema on the next start.

## Deployment options

### Docker Compose (recommended for a homelab)

The repo ships a minimal [`compose.yml`](../compose.yml). It builds the image
locally from the [`Dockerfile`](../Dockerfile) on the first `up`, so there are no
prerequisites beyond Docker:

```sh
docker compose up -d --build
# Then open http://localhost:8080 and create your account on the first screen.
#
# Optional, for the shell path instead: create the account and import from the CLI
# (make an export reachable first, see the commented volume in compose.yml).
docker compose run --rm verve account create --email=you@example.com
docker compose run --rm verve import --account=you@example.com /import/export.zip
```

The image is [distroless](https://github.com/GoogleContainerTools/distroless)
and runs as a non-root user (uid 65532); the single `/data` volume is the data
directory.

### Standalone binary

No version is tagged yet, so there is no binary to download: build one with
`make dist` (see below). Once releases are published, a static binary for your
OS and architecture will need nothing beyond a data directory:

```sh
VERVE_DATA_DIR=/srv/verve ./verve serve --addr=:8080
```

Run it under your init system of choice (systemd, runit, a supervisor) pointed at
a persistent `VERVE_DATA_DIR`.

### Building from source

`make dist` builds the SPA and embeds it into the binary (see the
[Makefile](../Makefile)); `docker build -t verve .` produces the image.
