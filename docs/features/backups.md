# Backups

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Configuration and deployment](platform-configuration-and-deployment.md), [Notifications](notifications.md), [Observability](platform-observability.md)

## Summary

Verve backs up its database on a schedule, before every migration and on demand, using `VACUUM INTO` which produces a consistent copy while the application is running. Backups are kept in the data directory with a retention policy and can be restored with the CLI.

## User stories

- As a self-hoster, I want automatic backups without setting up anything.
- As a self-hoster, I want to copy backups off-site with my usual tools (restic, rclone, Borg).
- As an operator, I want to roll back after a failed upgrade.

## Scope

### In scope

- Scheduled, pre-migration and on-demand backups.
- Retention.
- CLI backup and restore.
- Backup status in admin UI and metrics.

### Out of scope

- Pushing backups to remote storage (future).
- Backing up uploaded map files (they can be downloaded again).

## Functional requirements

- **FR-1**: A backup is produced with `VACUUM INTO` into a temporary file, then packed into a zstd-compressed tar archive and atomically renamed to `backups/verve-<version>-<UTC timestamp>-<reason>.tar.zst`, where reason is `scheduled`, `pre-migration` or `manual`.
- **FR-2**: Scheduled backups run daily at 03:00 in the instance timezone by default. Schedule configurable with a cron expression (`VERVE_BACKUP_SCHEDULE`) or disabled.
- **FR-3**: Retention: keep the last 7 scheduled backups by default (`VERVE_BACKUP_KEEP`). Pre-migration backups are kept for the last 3 migrations. Manual backups are never deleted automatically.
- **FR-4**: The archive contains the database and `secret.key`, because encrypted settings are useless without it. The documentation warns that backup files are therefore sensitive.
- **FR-5**: `verve backup` creates a manual backup and prints its path. It works while the server is running.
- **FR-6**: `verve restore <file>` refuses to run while the server holds the data directory lock. It validates the archive, keeps a copy of the current database as `pre-restore`, restores, then runs migrations if the backup comes from an older version.
- **FR-7**: Backups from a newer Verve version than the binary are refused.
- **FR-8**: Admin UI shows the list of backups (date, reason, size), a "Back up now" button and the next scheduled run. Download of a backup is available to admins.
- **FR-9**: A failed backup creates a failed job visible to admins. From M3, it also triggers an admin notification when notifications are configured.
- **FR-10**: Before a backup, Verve checks free disk space and aborts with a clear error if the estimated size does not fit.

## UX notes

- Admin backups page with a short reminder that off-site copies are the user's responsibility, with links to documentation examples.

## Data and API notes

- Admin endpoints: list, create, download.
- Metric: last successful backup timestamp and size.
- An archive is named `verve-<version>-<UTC timestamp>-<reason>.tar.zst`, for example `verve-0.4.0-20261110T140305Z-manual.tar.zst`. The timestamp is the start of the snapshot, in the basic ISO 8601 format, without the colons that Windows refuses in file names. The archive and its files have the permissions `0600`. It holds `verve.db`, and `secret.key` when the data directory has that file: with the key in `VERVE_SECRET_KEY`, or before the server generated the file, the archive holds only the database.
- `store.Store` takes the snapshot with `Snapshot(ctx, path)`. The SQLite implementation runs `VACUUM INTO` on a connection of its own: on the writer, it would hold off every other write for the whole copy, and the readers are `query_only`, which refuses it. In WAL mode, its read transaction blocks no writer.
- `internal/backup` streams the snapshot through `archive/tar` into a zstd encoder (`github.com/klauspost/compress/zstd`) limited to one goroutine, so that small hardware keeps its other cores for the server.
- A backup works in a temporary directory of the backups directory, `.verve-backup-*`, with the permissions `0700`, and holds a lock on a file inside it while it runs. Once the archive is complete, it is linked into the backups directory under its final name, which fails rather than replace the archive of a backup made in the same second, then the audit event is recorded. On failure, the archive and the temporary directory are removed. A temporary directory that cannot be removed after a successful backup does not fail it. A backup first removes the temporary directories whose lock is free: those of backups that crashed. It removes them and creates its own under the lock of the backups directory, `.verve-backup.lock`, so that backups running at the same time keep each other's.
- FR-10: the estimated size is the size of the database and of its WAL, which bounds the snapshot. It must fit in the space available in the backups directory, or the backup aborts with both, for example `not enough free space: the backup needs about 4.5 MB, 1.2 MB available in /data/backups`. The free space comes from `statfs` on Unix and `GetDiskFreeSpaceEx` on Windows.
- A backup records `backup.created` in the audit log, see [Audit log](audit-log.md).
- `verve backup` opens the database in shared mode, so that it runs next to `serve`, and prints the path of the archive on stdout.

## Edge cases

- Backup during a large import: `VACUUM INTO` sees a consistent snapshot. The import continues.
- Very large database on small storage: FR-10 prevents filling the disk.
- Restoring a backup of another instance: works, including users and secret key.
- Key set through `VERVE_SECRET_KEY`: there is no `secret.key` file, so the archive holds only the database and the operator keeps the key.

## Acceptance criteria

- [ ] Backups taken during writes restore to a consistent database.
- [ ] Retention deletes only scheduled and old pre-migration backups.
- [ ] `verve restore` refuses to run while the server is running.
- [ ] A restored older backup is migrated automatically.

## Future ideas

- S3-compatible remote targets.
- Encrypted backups with a user-provided passphrase.
