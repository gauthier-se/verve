# Run the Verve binary

Every [GitHub release](https://github.com/gauthier-se/verve/releases) contains a standalone binary with the web app embedded, for Linux (amd64, arm64), macOS (amd64, arm64) and Windows (amd64). It needs no runtime and no separate database. This guide installs it as a systemd service on Linux.

## Install the binary

Download the archive of your architecture (`amd64` or `arm64`), check it against the release checksums and install the binary:

```sh
version=0.1.0
arch=amd64
curl -fsSLO "https://github.com/gauthier-se/verve/releases/download/v$version/verve_${version}_linux_$arch.tar.gz"
curl -fsSLO "https://github.com/gauthier-se/verve/releases/download/v$version/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "verve_${version}_linux_$arch.tar.gz" verve
sudo install -m 0755 verve /usr/local/bin/verve
```

`checksums.txt` is signed with cosign: see [Contributing](../../CONTRIBUTING.md#releases) to verify it.

## Create a dedicated user

Verve needs no privileges. Run it as its own system user, without a home directory or a login shell:

```sh
sudo useradd --system --user-group --no-create-home --shell /usr/sbin/nologin verve
```

## Write the environment file

Create `/etc/verve/verve.env`. The [configuration reference](../features/platform-configuration-and-deployment.md#functional-requirements) (FR-3) lists every variable.

```sh
VERVE_PUBLIC_URL=https://verve.example.com
VERVE_LISTEN_ADDR=127.0.0.1:8080
VERVE_TRUSTED_PROXIES=127.0.0.1
```

These values fit a reverse proxy on the same machine: Verve listens on the loopback interface only and trusts forwarded headers from it. See the [reverse proxy guide](reverse-proxy.md). Without a proxy, set `VERVE_PUBLIC_URL` to the URL users reach Verve at, such as `http://192.168.1.10:8080`, and remove the last two lines: Verve then listens on `:8080`.

Only root needs to read this file, because systemd reads it before dropping privileges:

```sh
sudo chmod 0600 /etc/verve/verve.env
```

## Install the systemd unit

Create `/etc/systemd/system/verve.service`:

```ini
[Unit]
Description=Verve health dashboard
Documentation=https://github.com/gauthier-se/verve/blob/main/docs/deployment/binary.md
Wants=network-online.target
After=network-online.target

[Service]
ExecStart=/usr/local/bin/verve serve
User=verve
Group=verve
# Creates /var/lib/verve, owned by the user above.
StateDirectory=verve
StateDirectoryMode=0750
Environment=VERVE_DATA_DIR=/var/lib/verve
EnvironmentFile=/etc/verve/verve.env
Restart=on-failure
RestartSec=5
# verve serve waits up to 30 seconds for in-flight requests when it stops.
TimeoutStopSec=40
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict

[Install]
WantedBy=multi-user.target
```

`VERVE_DATA_DIR` must match `StateDirectory`: its default, `/data`, is meant for the Docker image. Keep this directory on a local disk, because the database uses SQLite in WAL mode, which does not work on network file systems.

Start the service and check it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now verve
systemctl status verve
curl -f http://127.0.0.1:8080/healthz
journalctl -u verve -f
```

The logs in the journal show the startup warnings and one line per request.

## Upgrades

Schema migrations run automatically at startup, and downgrades are not supported. Automatic backups arrive with the [backups feature](../features/backups.md) in milestone M1. Until then, copy the data directory when you upgrade.

Download and extract the new version as in [Install the binary](#install-the-binary), then replace the running one:

```sh
sudo systemctl stop verve
sudo cp -a /var/lib/verve /var/lib/verve.bak
sudo install -m 0755 verve /usr/local/bin/verve
sudo systemctl start verve
```

To roll back, stop the service, replace `/var/lib/verve` with this copy and install the previous binary.
