# Job worker

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Apple Health XML import](ingestion-xml-import.md), [Storage and aggregates](data-storage-and-aggregates.md), [Backups](backups.md), [Accounts and administration](accounts-and-administration.md), [ADR 0001](../decisions/0001-sqlite-behind-storage-interface.md)

## Summary

Background work runs inside the single binary, on a persistent `jobs` table: imports, aggregate recomputation, backups, account deletion and cleanups. Jobs survive restarts and crashes, and no external broker is needed. Features bring their own job kinds, this feature defines how they run.

## User stories

- As an operator, I want background work to survive a restart or a crash.
- As a user, I want my imports to run one at a time, in arrival order.
- As an admin, I want to see which jobs failed and why.

## Scope

### In scope

- Persistent queue, leases, retries, cancellation, graceful shutdown.
- Serial keys, across the server and CLI processes.
- Recurring jobs.
- Failed jobs available to admins.

### Out of scope

- External brokers, and several servers on one database.
- Job kinds: each feature specifies its own.
- Priorities between jobs.

## Functional requirements

- **FR-1**: A job has a kind, a JSON payload, a status (`queued`, `running`, `completed`, `failed`, `cancelled`), an attempt counter, a `run_at` time, a lease (`locked_until`), an optional user id, an optional serial key, its last error and its creation and completion times.
- **FR-2**: Enqueuing a job is a database write. It can take part in the transaction of the data it follows, so that committed data never misses its job.
- **FR-3**: The server runs a small worker pool (2 workers by default). A worker claims the oldest due job atomically, setting it to `running` with a lease. Workers wake up at once on a job enqueued by the same process, and otherwise poll every few seconds, so that a job enqueued by a CLI process is picked up too.
- **FR-4**: The lease lasts 60 seconds and is renewed by a heartbeat while the job runs. A job whose lease has expired is due again, without counting as a failed attempt: this is how a crash is recovered. Handlers must therefore be idempotent or resumable. A handler whose heartbeat fails has its context cancelled, so that the job does not run twice.
- **FR-5**: A job that returns an error is retried with exponential backoff, from 30 seconds up to 1 hour, up to the maximum number of attempts of its kind (5 by default), then marked `failed`. A handler can report a permanent error, such as a corrupted file, which fails the job at once.
- **FR-6**: Jobs that share a serial key run one at a time, oldest first. Imports use one key per user, which makes it the per-user import lock of [Ingestion pipeline](ingestion-pipeline.md) FR-4. The key is checked in the database when a job is claimed, so it holds across processes.
- **FR-7**: A CLI command that runs a job in-process, such as `verve import xml`, waits while another job holds its serial key, then creates and claims its job atomically. It renews the lease like a worker. If the process dies, the lease expires and the server resumes the job.
- **FR-8**: Recurring jobs are declared in code with a cron expression or an interval, and their next run is stored in the database. Only the server schedules them, since it holds the data directory lock. A run missed during downtime happens once after startup, however many were missed.
- **FR-9**: Cancelling a queued job takes effect at once. Cancelling a running job cancels its context, and the handler stops at the next batch boundary.
- **FR-10**: On shutdown, workers stop claiming jobs and cancel the running handlers. An interrupted job returns to `queued` without counting as a failed attempt.
- **FR-11**: The worker starts after migrations are applied, and stops before the database closes.
- **FR-12**: Admins can list failed jobs with their kind, attempts, times and last error. Payloads are never exposed. Job errors, like logs, never contain health values or secrets.
- **FR-13**: Each job writes one structured log line when it ends, with its kind, id, status and duration, never its payload.
- **FR-14**: Finished jobs are purged by a recurring job: completed after 7 days, failed after 30 days.

## UX notes

- The admin screen for failed jobs is part of the remaining M1 screens, see the [roadmap](../roadmap.md).
- Users follow their imports through the import history, not through jobs.

## Data and API notes

- Tables: `jobs`, `job_schedules`.
- Endpoint: `GET /api/v1/admin/jobs`.
- Handlers are registered per kind in `internal/app`. Tests drive the worker with the fake clock.

## Edge cases

- Lease expired while the handler is still alive, for example after a long pause of the process: the failed heartbeat cancels the handler before another worker finishes the job.
- Job kind unknown to the running binary, for example after restoring an older version: the job is marked `failed` with a clear error.
- CLI and server writing at the same time: each process has its own writer connection, and `busy_timeout` makes them wait for each other.
- Worker pool busy with a long import: serial keys are per user, so another worker stays available for other jobs.

## Acceptance criteria

- [ ] A job enqueued before a crash runs after the restart.
- [ ] A job whose process died is picked up after its lease expires.
- [ ] Two jobs with the same serial key never run at the same time, including when one of them runs in a CLI process.
- [ ] A job that keeps failing ends as `failed` with its error visible to admins.
- [ ] A recurring job missed during downtime runs once after startup.

## Future ideas

- Priorities, so that a large import never delays a small recomputation.
