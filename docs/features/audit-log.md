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
  - administration: user invited, enabled, disabled, role changed, password reset link generated, 2FA reset, instance setting changed, backup created, downloaded and restored.
- **FR-3**: Payloads never contain health values, passwords, tokens, session ids or secret settings values.
- **FR-4**: Users see events where they are the actor or the subject, in their security page.
- **FR-5**: Admins see all events with filters (type, actor, subject, date range).
- **FR-6**: Retention: 365 days by default, configurable. Purge runs as a periodic job.
- **FR-7**: Events are append-only through the application. There is no endpoint to edit or delete individual events.
- **FR-8**: Failed login events for unknown usernames are recorded with the attempted username truncated to 64 characters and without linking to a user.

## UX notes

- User view: "Security activity" list with icons per category, newest first.
- Admin view: TanStack Table with filters and pagination.

## Data and API notes

- `audit_events` table indexed by time, actor and subject, without foreign keys, since events outlive users.
- Events are built only by the constructors of `internal/audit`, whose payloads are closed lists of fields. A table test checks every constructor against the sensitive keys of `internal/logging`, the one definition of that rule.
- `audit.Record` runs on `tx.AuditEvents()` inside the `WithTx` of the action, so that an event commits or rolls back with it.
- The client IP and the user agent, truncated to 256 characters, come from the request context. They are empty for jobs and CLI commands.
- A constructor can make its event aggregatable by giving it an aggregation key: the client IP for failed logins, so that trying many usernames does not escape aggregation. `Append` counts and writes in one writer transaction.
- Endpoints: `GET /api/v1/account/audit`, `GET /api/v1/admin/audit`.

## Edge cases

- Bursts of failed logins (brute force): events are recorded but aggregated after a threshold to avoid filling the table. Once 20 events of the same type and aggregation key happened in the last 10 minutes, the next one increments the `count` and updates the `last_at` of their latest row instead of adding one. The events of a row count as recent while its `last_at` is, so a burst whose latest row holds 20 events goes on until a quiet period of 10 minutes. Another client IP or another event type is never aggregated with it.
- Deleted user: their events are kept with the user id, displayed as "deleted user".

## Acceptance criteria

- [ ] Every event type listed in FR-2 is recorded by the corresponding action.
- [ ] No audit payload contains a health value or a secret.
- [ ] Retention purge removes only events older than the configured period.

## Future ideas

- Syslog or webhook forwarding of audit events.
