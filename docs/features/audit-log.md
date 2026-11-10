# Audit log

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Accounts and administration](accounts-and-administration.md), [Local accounts and sessions](auth-local-accounts-and-sessions.md), [Deletion and block rules](data-deletion-and-block-rules.md)

## Summary

An append-only record of security-relevant and data management events: logins, token changes, imports, deletions, admin actions. It helps users understand what happened to their account and admins investigate problems, without storing health values.

## User stories

- As a user, I want to see recent logins and token changes on my account.
- As an admin, I want to know who changed instance settings or disabled an account.
- As a user, I want to know when data was deleted and by which action.

## Scope

### In scope

- Event recording, retention, user and admin views.

### Out of scope

- Logging reads of health data.
- Export to external SIEM (future).

## Functional requirements

- **FR-1**: Each event stores: timestamp, actor (user id or `system`), subject user id when relevant, event type, client IP, user agent, and a small JSON payload with non-sensitive details.
- **FR-2**: Recorded event types include:
  - authentication: login success, login failure, logout, session revoked, password changed, password reset requested and completed, TOTP enabled, disabled and recovery code used, OIDC account linked and unlinked,
  - tokens: created, revoked, rejected because revoked, expired or user disabled,
  - data: import started, completed, failed, cancelled, data deleted (scope and counts), block rule created and removed, export created and downloaded, account deletion requested and completed,
  - administration: user created by the setup or the command line, user invited, enabled, disabled, role changed, password reset link generated, 2FA reset, instance setting changed, backup created, downloaded and restored.
- **FR-3**: Payloads never contain health values, passwords, tokens, session ids or secret settings values.
- **FR-4**: Users see events where they are the actor or the subject, in their security page.
- **FR-5**: Admins see all events with filters (type, actor, subject, date range).
- **FR-6**: Retention: 365 days by default, configurable. Purge runs as a periodic job.

  | Variable | YAML key | Default | Purpose |
  |---|---|---|---|
  | `VERVE_AUDIT_RETENTION_DAYS` | `audit.retention_days` | `365` | Days an event is kept, a whole number from 1 to 36500 (100 years) |

  The recurring job `audit.purge` runs daily and removes the events older than the retention period, in batches of at most 5,000 per transaction so that it never holds the writer for long. An event exactly as old as the retention period is kept, and the next purge removes it. An aggregated row is older than the retention period once its last event (`last_at`) is: it is kept while it holds a recent event, so a burst that never goes quiet keeps its row, older events included, for as long as it lasts.
- **FR-7**: Events are append-only through the application. There is no endpoint to edit or delete individual events.
- **FR-8**: Failed login events for unknown usernames are recorded with the attempted username truncated to 64 characters and without linking to a user. A failed login for an existing username or email has its user as subject, so that the user sees the attempts on their account.

## UX notes

- User view: "Security activity" list with icons per category, newest first.
- Admin view: TanStack Table with filters and pagination.

## Data and API notes

- `audit_events` table indexed by time, actor and subject, without foreign keys, since events outlive users.
- Events are built only by the constructors of `internal/audit`, whose payloads are closed lists of fields. A table test checks every constructor against the sensitive keys of `internal/logging`, the one definition of that rule.
- `audit.Record` runs on `tx.AuditEvents()` inside the `WithTx` of the action, so that an event commits or rolls back with it.
- The client IP and the user agent, truncated to 256 characters, come from the request context. They are empty for jobs and CLI commands.
- A constructor can make its event aggregatable by giving it an aggregation key: the client IP for failed logins, so that trying many usernames does not escape aggregation, together with the user for a login that exists, so that a row never counts the attempts on another account than the one it names. `Append` counts and writes in one writer transaction.
- The authentication events of the password login:
  - `auth.login_succeeded`: the user as actor, and the login method in the payload, such as `{"method": "password"}`.
  - `auth.login_failed`: no actor, the user of the login as subject, or none for a login that no user has (FR-8), and the attempted login in the payload, `{"attempted": "..."}`.
  - `auth.logout`: the user as actor and an empty payload. A logout without a valid session records nothing.
  - `auth.password_changed`: the user as actor and an empty payload. The other sessions that the change revokes record no event of their own. A wrong current password records nothing: it counts in the login rate limit instead.
  - `auth.session_revoked`: the user as actor, and the number of sessions revoked in the payload, `{"revoked": 1}` for one session. A revocation of the other sessions when there are none records nothing.
- The creation of a user, `user.created`: no actor, since no user acted (the setup comes before any user, the command line has none, and the invitee is not a user before accepting), the new user as subject, and their role and where they were created from in the payload, `{"role": "admin", "source": "setup"}`, `"source": "cli"` or `"source": "invitation"`. The setup and the accepted invitation record the client of their request. A rejected setup or accept records nothing.
- An invitation, `user.invited`: the admin as actor, no subject, since the user does not exist until the invitation is accepted, and the invited username and role in the payload, `{"username": "bob", "role": "member"}`.
- The administration of an account, `user.disabled`, `user.enabled`, `user.role_changed` and `user.password_reset`: the admin as actor, or the system for the command line, the user as subject, and an empty payload, except for a role change, whose payload has the former and the new role, `{"from": "member", "to": "admin"}`. The API records the client of its request. A change that the user already has records nothing.
- A password reset link, `user.password_reset_link_created`: the admin as actor, the user as subject and an empty payload. Its use, `auth.password_reset_completed`: the user as actor, no subject and an empty payload, like `auth.password_changed`. Both record the client of their request. A rejected reset records nothing.
- An account deletion, `user.deletion_requested`: the user who asked as actor, themselves or an admin, the user as subject and an empty payload, with the client of the request. Its end, `user.deletion_completed`: no actor, since the job of the system removes the account, the user as subject and an empty payload, recorded once, in the transaction that removes the user. The events about the user stay, as for any deleted user.
- A backup, `backup.created`: no actor, since the command line has no user and the schedule and the migrations are the system, no subject, and the reason and the file name of the archive in the payload, `{"reason": "manual", "file": "verve-0.4.0-20261110T140305Z-manual.tar.zst"}`. It is recorded once the archive is in place, and a failure to record it removes the archive. See [Backups](backups.md).
- Endpoints, paginated as described in [Architecture](../architecture.md#pagination), newest first by first occurrence, then by id:
  - `GET /api/v1/account/audit`: the events whose actor or subject is the user of the session (FR-4). An admin also gets only their own events here.
  - `GET /api/v1/admin/audit`: every event, for admins only (403 `forbidden` for a member), with the filters `type`, `actor` and `subject` (user ids, so the events of the system are found by their type), `from` and `to` (RFC 3339 times), which combine (FR-5). An aggregated event is in the time range while one of its occurrences is: its first one before `to`, and its last one at or after `from`. A `to` that is not after `from` answers 422 `validation_failed`.
- An event has `id`, `occurredAt`, `type`, `actor` (left out for the system), `subject` (left out when there is none), `clientIp` and `userAgent` (left out outside requests), `payload`, `count` and `lastAt`. `count` is above 1 for an aggregated event, whose `lastAt` is the time of its last occurrence. Otherwise `lastAt` is `occurredAt`.
- `actor` and `subject` have the `id` and the `username` of the user, and `deleted`: the event of a user who no longer exists stays listed with `deleted: true` and without a username.

## Edge cases

- Bursts of failed logins (brute force): events are recorded but aggregated after a threshold to avoid filling the table. Once 20 events of the same type and aggregation key happened in the last 10 minutes, the next one increments the `count` and updates the `last_at` of their latest row instead of adding one. The events of a row count as recent while its `last_at` is, so a burst whose latest row holds 20 events goes on until a quiet period of 10 minutes. Another client IP, another event type or, for a failed login, another user is never aggregated with it.
- Deleted user: their events are kept with the user id, displayed as "deleted user".

## Acceptance criteria

- [ ] Every event type listed in FR-2 is recorded by the corresponding action.
- [ ] No audit payload contains a health value or a secret.
- [ ] Retention purge removes only events older than the configured period.

## Future ideas

- Syslog or webhook forwarding of audit events.
