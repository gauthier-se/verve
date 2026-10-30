# Run Verve with Docker

Verve is published to GitHub Container Registry as `ghcr.io/gauthier-se/verve`, for `linux/amd64` and `linux/arm64`. The image is distroless, runs as a non-root user and reports its health to Docker.

## Quick start

Download the example [`compose.yaml`](../../compose.yaml) and start it:

```sh
curl -fsSLO https://raw.githubusercontent.com/gauthier-se/verve/main/compose.yaml
docker compose up -d
docker compose ps
```

Once `docker compose ps` reports `healthy`, usually within a few seconds, open http://localhost:8080.

The file works as is for a first try over plain HTTP. Verve then logs a warning that `VERVE_PUBLIC_URL` is not set. To use it for real, put it behind a reverse proxy that terminates TLS, as described in the [reverse proxy guide](reverse-proxy.md), which also explains the two placeholders of the file:

| Variable | Set it to |
|---|---|
| `VERVE_PUBLIC_URL` | The URL users reach Verve at, without a path, for example `https://verve.example.com` |
| `VERVE_TRUSTED_PROXIES` | The addresses or CIDRs of your reverse proxy, for example the Docker network CIDR |

An empty value is ignored, so the two variables can stay empty until you need them.

## Configuration

Every setting is a `VERVE_*` environment variable, listed in the [configuration reference](../features/platform-configuration-and-deployment.md#functional-requirements) (FR-3). Print the effective values and where each one comes from with:

```sh
docker compose run --rm verve config check
```

On invalid configuration, this command prints every error and exits with status 1.

## Volumes and permissions

Everything Verve stores lives in `/data`: the SQLite database, uploads, exports, backups and map files. The [layout](../features/platform-configuration-and-deployment.md#functional-requirements) is described in FR-4. The server runs as UID and GID `65532`, so that directory must be writable by this user.

| | Named volume | Bind mount |
|---|---|---|
| In `compose.yaml` | `verve-data:/data` | `/srv/verve:/data` |
| Permissions | None to set: Docker gives a new volume the owner of `/data` in the image | The directory must be owned by `65532:65532` |
| Best for | Most installations | Backing up with your existing file tools |

To use a bind mount, create the directory before starting Verve, and copy any existing data into it first:

```sh
sudo mkdir -p /srv/verve
sudo chown -R 65532:65532 /srv/verve
```

With the wrong owner, the container stops at startup and Docker restarts it in a loop. `docker compose logs verve` then shows:

```text
Error: data directory /data is not writable: open /data/.write-check-951082322: permission denied
```

Keep `/data` on a local disk: the database uses SQLite in WAL mode, which does not work on network file systems such as NFS or SMB. Only one Verve instance can use a data directory at a time.

## Upgrades

Pull the new image and recreate the container:

```sh
docker compose pull
docker compose up -d
```

Schema migrations run automatically at startup. The server already listens while they run and answers 503 until they are applied. Downgrades are not supported.

`latest` follows the latest release. To decide when you upgrade, replace it in `compose.yaml` with a version such as `0.1.0`, or a partial version such as `0.1`. The `edge` tag follows the main branch and is meant for testing only.

### Backups

Automatic backups, including one before every migration, arrive with the [backups feature](../features/backups.md) in milestone M1. Until then, copy the data directory yourself before upgrading:

```sh
docker compose stop verve
docker compose cp verve:/data ./verve-backup
docker compose start verve
```

To roll back, restore this copy and go back to the previous image version in `compose.yaml`:

```sh
docker compose stop verve
docker run --rm --volumes-from "$(docker compose ps --all --quiet verve)" -v "$PWD/verve-backup:/backup:ro" alpine \
  sh -c 'rm -rf /data/* && cp -R /backup/. /data/ && chown -R 65532:65532 /data'
docker compose up -d
```
