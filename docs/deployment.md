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
  artifacts/      large referenced files (GPX workout routes, ECG) — ADR 0004
```

It is created on first run if missing, and database migrations auto-apply on
every startup — there is no manual migrate step for the operator.

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

Verve is multi-user with strict isolation (ADR 0007); there is no default
account and no web-based sign-up. Create the first account from the CLI, then
sign in.

```sh
# 1. Point at a data directory (or rely on the ./data default).
export VERVE_DATA_DIR=/srv/verve

# 2. Create your account — this also creates the data dir and the database.
verve account create --email=you@example.com

# 3. Import an Apple Health export (Health app → profile → Export All Health Data).
verve import --account=you@example.com ~/Downloads/export.zip

# 4. Start the server and open http://localhost:8080
verve serve --addr=:8080
```

Re-importing the same export is idempotent — only new readings are added
(ADR 0006), so you can re-run step 3 after each new snapshot.

## Backup

Because everything lives in one directory, **backup is copying the data dir** —
no `pg_dump`, no export step:

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
docker compose run --rm verve account create --email=you@example.com
# make an export reachable (see the commented volume in compose.yml), then:
docker compose run --rm verve import --account=you@example.com /import/export.zip
```

The image is [distroless](https://github.com/GoogleContainerTools/distroless)
and runs as a non-root user (uid 65532); the single `/data` volume is the data
directory.

### Standalone binary

Grab a static binary for your OS/arch from the releases, set `VERVE_DATA_DIR`,
and run it — no dependencies:

```sh
VERVE_DATA_DIR=/srv/verve ./verve account create --email=you@example.com
VERVE_DATA_DIR=/srv/verve ./verve serve --addr=:8080
```

Run it under your init system of choice (systemd, runit, a supervisor) pointed at
a persistent `VERVE_DATA_DIR`.

### Building from source

`make dist` builds the SPA and embeds it into the binary (see the
[Makefile](../Makefile)); `docker build -t verve .` produces the image.
