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

- **FR-1**: A backup is produced with `VACUUM INTO` into a temporary file, then packed into a zstd-compressed tar archive and atomically renamed to `backups/verve-<version>-<UTC timestamp>-<reason>.tar.zst`, where reason is `scheduled`, `pre-migration`, `manual` or `pre-restore`.
- **FR-2**: Scheduled backups run daily at 03:00 in the instance timezone by default. Schedule configurable with a cron expression (`VERVE_BACKUP_SCHEDULE`) or disabled.

  | Variable | YAML key | Default | Purpose |
  |---|---|---|---|
  | `VERVE_BACKUP_SCHEDULE` | `backup.schedule` | `0 3 * * *` | When scheduled backups run: a cron expression with the five standard fields, or `off` |
  | `VERVE_BACKUP_KEEP` | `backup.keep` | `7` | Number of scheduled backups kept, a whole number of at least 1 |

  The cron expression is read in the local time of the process. In a container, that is UTC unless `TZ` is set, for example `TZ=Europe/Paris`: the binary embeds the time zone database, so the image needs none. Descriptors such as `@daily` and time zone prefixes such as `CRON_TZ=` are refused, and so is an expression that no date matches, such as `0 3 30 2 *`: configuration validation fails and names the setting. `off` disables scheduled backups and forgets their next run. The schedule follows the recurring jobs of [Background jobs](platform-jobs.md): a run missed during downtime happens once after startup.
- **FR-3**: Retention: keep the last 7 scheduled backups by default (`VERVE_BACKUP_KEEP`). Pre-migration backups are kept for the last 3 migrations. Manual and pre-restore backups are never deleted automatically.

  A scheduled backup is the recurring job `backup.run`. Once its archive is in place, it removes the scheduled archives of the backups directory but the newest `VERVE_BACKUP_KEEP`, the new one included. It reads the reason and the UTC timestamp from the name of each file, orders by that timestamp whatever the version, and never removes a manual, pre-migration or pre-restore archive, a directory, or a file whose name it cannot parse, such as an archive renamed by hand. A failed backup runs no retention. The retention of pre-migration backups comes with them, and is separate: scheduled backups never count against it.

  A pre-migration backup is taken by `verve serve` and `verve migrate` before the first pending migration, when the database already has a schema: a brand new database, at version 0, has nothing to back up, and a schema up to date takes no backup. A failed backup applies no migration: the command fails with the reason, and `serve` stops without becoming ready. Once the migrations are applied, it removes the pre-migration archives but the newest 3, the new one included, the same way. A migration that fails removes none: only the migrations that succeed count, so that restarts that keep failing never remove the backup from before the upgrade, while the migrations that succeeded before the failure stay applied. A failure of this retention fails the command, and the migrations stay applied. At startup, `/readyz` answers 503 with the reason `starting: applying migrations` while the backup runs, see [Observability](platform-observability.md).
- **FR-4**: The archive contains the database and `secret.key`, because encrypted settings are useless without it. The documentation warns that backup files are therefore sensitive.
- **FR-5**: `verve backup` creates a manual backup and prints its path. It works while the server is running.
- **FR-6**: `verve restore <file>` refuses to run while the server holds the data directory lock. It validates the archive, keeps a copy of the current database as `pre-restore`, restores, then runs migrations if the backup comes from an older version.

  `verve restore` takes the data directory lock, so that it fails while `serve` runs, like `verve migrate`: `another Verve process is using /data: stop verve serve before restoring a backup`. It refuses the archive, and changes nothing, when the file does not read to its end (corrupted, truncated, or followed by other data), when it holds an entry other than a regular file named `verve.db` or `secret.key`, a path included, or one of them twice, when it holds no `verve.db`, when the database fails `PRAGMA integrity_check`, when it has no schema of Verve or one newer than the binary (FR-7), and when its `secret.key` is not a valid key. Then it backs up the current database and key as `pre-restore`, unless the data directory has no database yet or one without schema, replaces them with those of the archive, and applies the pending migrations of a backup from an older version. A backup without `secret.key` keeps the current key file. It prints the path of the pre-restore backup, what it restored and the migrations it applied.

  A failure once the archive is accepted, such as a migration that fails, can leave the restored database in place: the pre-restore backup is then the way back.
- **FR-7**: Backups from a newer Verve version than the binary are refused. The schema version of the database decides, not the version in the name of the file.
- **FR-8**: Admin UI shows the list of backups (date, reason, size), a "Back up now" button and the next scheduled run. Download of a backup is available to admins.
- **FR-9**: A failed backup creates a failed job visible to admins. From M3, it also triggers an admin notification when notifications are configured.

  The job `backup.run` has a single attempt: a failure marks it `failed` at once, with its error, instead of retrying it, and the next scheduled run tries again. A failure of retention after the backup also fails the job, although the archive stays.
- **FR-10**: Before a backup, Verve checks free disk space and aborts with a clear error if the estimated size does not fit.

## UX notes

- Admin backups page with a short reminder that off-site copies are the user's responsibility, with links to documentation examples.

## Data and API notes

- Admin endpoints, for admins only (403 `forbidden` for a member, 401 without a session). Until the admin screen of M1-E12, backups are managed through the API.
  - `GET /api/v1/admin/backups` answers `{"items": [...], "nextRun": "..."}`: the archives of the backups directory, newest first by the timestamp of their name, each with its `name`, `createdAt` (that timestamp), `reason` and `size` in bytes, and the next run of the scheduled backups, left out when they are `off`. The list is not paginated: retention keeps it short. It leaves out the directories, the links, and the files whose name is not the one of an archive or holds another character than an ASCII letter, a digit, `.`, `-`, `_` or `+`.
  - `POST /api/v1/admin/backups` takes no body, queues the job `backup.create` of a manual backup and answers 202 with `{"jobId": 42}`. The job has a single attempt, like `backup.run` (FR-9), and its serial key makes manual backups run one at a time. It records `backup.created` with the admin as actor once the archive is in place.
  - `GET /api/v1/admin/backups/{name}/download` streams the archive of name as `application/zstd`, with `Content-Disposition: attachment`, its `Content-Length` and `Cache-Control: no-store`, so that a large archive never loads into memory. It records `backup.downloaded` with the client of the request before sending the archive. A name that the list would leave out, such as one with a path separator, and a name that no archive has, answer 404 `not_found` and record nothing.
- Metric: last successful backup timestamp and size.
- An archive is named `verve-<version>-<UTC timestamp>-<reason>.tar.zst`, for example `verve-0.4.0-20261110T140305Z-manual.tar.zst`. The timestamp is the start of the snapshot, in the basic ISO 8601 format, without the colons that Windows refuses in file names. The archive and its files have the permissions `0600`. It holds `verve.db`, and `secret.key` when the data directory has that file: with the key in `VERVE_SECRET_KEY`, or before the server generated the file, the archive holds only the database.
- `store.Store` takes the snapshot with `Snapshot(ctx, path)`. The SQLite implementation runs `VACUUM INTO` on a connection of its own: on the writer, it would hold off every other write for the whole copy, and the readers are `query_only`, which refuses it. In WAL mode, its read transaction blocks no writer.
- `internal/backup` streams the snapshot through `archive/tar` into a zstd encoder (`github.com/klauspost/compress/zstd`) limited to one goroutine, so that small hardware keeps its other cores for the server.
- A backup works in a temporary directory of the backups directory, `.verve-backup-*`, with the permissions `0700`, and holds a lock on a file inside it while it runs. Once the archive is complete, it is linked into the backups directory under its final name, which fails rather than replace the archive of a backup made in the same second, then the audit event is recorded, except for a pre-migration backup, see below. On failure, the archive and the temporary directory are removed. A temporary directory that cannot be removed after a successful backup does not fail it. A backup first removes the temporary directories whose lock is free: those of backups that crashed. It removes them and creates its own under the lock of the backups directory, `.verve-backup.lock`, so that backups running at the same time keep each other's.
- FR-10: the estimated size is the size of the database and of its WAL, which bounds the snapshot. It must fit in the space available in the backups directory, or the backup aborts with both, for example `not enough free space: the backup needs about 4.5 MB, 1.2 MB available in /data/backups`. The free space comes from `statfs` on Unix and `GetDiskFreeSpaceEx` on Windows.
- A backup records `backup.created` in the audit log, see [Audit log](audit-log.md). A pre-migration backup records it once the migrations are applied, because the schema before them may not hold the event: when a migration fails, the archive stays and no event is recorded. The hook runs before the store is used, so the snapshot is taken on the database directly.
- `verve backup` opens the database in shared mode, so that it runs next to `serve`, and prints the path of the archive on stdout.
- `verve restore` extracts the archive into a temporary directory of the data directory, `.verve-restore-*`, with the permissions `0700`, so that its files move into place with an atomic rename. It removes the temporary directories that a crashed restore left, which it can do under the data directory lock. It writes only the entries `verve.db` and `secret.key`, under these constant names: the name of an entry is never joined with a directory. A pre-restore backup is taken like the others, with the current database even when it is newer than the binary, and records no audit event in the database it replaces. The restore removes the `-wal` and `-shm` files of the replaced database, which SQLite would otherwise apply to the restored one, then renames the extracted files over the current ones. The migrations take no pre-migration backup: the archive restored is the way back, and the retention of pre-migration backups could remove it. Once they are applied, the restored database records `backup.created` for the pre-restore backup and `backup.restored`, see [Audit log](audit-log.md).

## Edge cases

- Backup during a large import: `VACUUM INTO` sees a consistent snapshot. The import continues.
- Very large database on small storage: FR-10 prevents filling the disk.
- Restoring a backup of another instance: works, including users and secret key.
- Rolling back a failed upgrade: the previous binary restores the pre-migration backup. The current database, newer than that binary, is kept as the pre-restore backup.
- A current database that cannot be read: the pre-restore backup fails, so the restore stops and changes nothing. Moving `verve.db` out of the data directory first lets the restore run, without pre-restore backup.
- Key set through `VERVE_SECRET_KEY`: there is no `secret.key` file, so the archive holds only the database and the operator keeps the key.
- Restoring an archive that holds `secret.key` while `VERVE_SECRET_KEY` is set: the file is restored, but the configured key still wins. Set `VERVE_SECRET_KEY` to the key of the instance the backup comes from, or unset it to use the restored file.

## Acceptance criteria

- [ ] Backups taken during writes restore to a consistent database.
- [ ] Retention deletes only scheduled and old pre-migration backups.
- [ ] `verve restore` refuses to run while the server is running.
- [ ] A restored older backup is migrated automatically.

## Future ideas

- S3-compatible remote targets.
- Encrypted backups with a user-provided passphrase.
