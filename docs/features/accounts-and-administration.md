# Accounts and administration

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Local accounts and sessions](auth-local-accounts-and-sessions.md), [API tokens](api-tokens.md), [Audit log](audit-log.md), [OIDC](auth-oidc.md)

## Summary

Verve is multi-user for a household. Each user's health data is isolated. An admin creates accounts by invitation (no public sign-up), manages instance settings and sees technical metadata, but cannot see other users' health data.

## User stories

- As the person installing Verve, I want to create the first admin account securely.
- As an admin, I want to invite my partner so they get their own account and their own data.
- As a member, I want my health data to stay private from the admin.
- As a user, I want to delete my account and all my data.

## Scope

### In scope

- First-run setup.
- Invitations, roles, disabling and deleting accounts.
- Admin views with metadata only.
- Instance settings page.
- Self-service account deletion.

### Out of scope

- Public sign-up.
- Sharing data between accounts (future).
- Admin access to other users' health data, including "impersonation".

## Functional requirements

- **FR-1**: When no user exists, the server prints a one-time setup token in its logs at startup. The `/setup` page requires this token to create the first admin. This prevents someone else on the network from claiming the instance first.
- **FR-2**: Alternatively, the first admin can be created with `verve user create --admin`, which disables the setup page.
- **FR-3**: Roles: `admin` and `member`. Admins have all member capabilities for their own data plus administration.
- **FR-4**: Admins invite users by creating an invitation (username, optional email, role). Verve produces a single-use invitation link valid for 72 hours, which the admin copies or sends by email when SMTP is configured. The invitee chooses their own password through the link.
- **FR-5**: Admins can disable and re-enable accounts. Disabling revokes all sessions and makes all API tokens of that user fail until re-enabled.
- **FR-6**: Admins can generate a single-use password reset link for a user (valid 24 hours). The admin never sets or sees the password.
- **FR-7**: Self-service password reset by email is available when SMTP is configured (see [Notifications](notifications.md)).
- **FR-8**: Admins can promote and demote users. The last active admin cannot be demoted, disabled or deleted.
- **FR-9**: The admin user list shows metadata only: username, email, role, status, created date, last login, number of samples, storage used, last ingestion per origin. No health values, charts or plans of other users.
- **FR-10**: The storage layer exposes no operation that reads another user's health data, so the restriction does not rely on the UI alone.
- **FR-11**: A user can delete their own account after re-entering their password (or re-authenticating through OIDC). Deletion removes all their data: samples, workouts, aggregates, plans, dashboards, tokens, sessions, uploads, exports, notifications settings. Audit events about the deletion are kept without health content.
- **FR-12**: An admin can delete another user's account with a confirmation that requires typing the username.
- **FR-13**: Account deletion runs as a background job and reports completion. The account is disabled immediately.
- **FR-14**: The instance settings page groups admin-managed settings: general (public URL warning, default language), map, SMTP, backups, OIDC, proxy header, 2FA enforcement.

## UX notes

- Setup wizard: setup token, admin account, default language and units, then a short guide to importing data.
- Admin area is a separate section in navigation, visible only to admins.
- Deleting data is always behind a confirmation dialog describing exactly what will be removed.

## Data and API notes

- `users` (username unique, email optional and unique when present, role, status).
- `invitations` (token hash, role, expiry, used at).
- Admin endpoints are under `/api/v1/admin/...` and require the admin role.

## Edge cases

- Invitation link used twice: second use fails with a clear message.
- Username collision with an OIDC or proxy header identity: handled by the linking rules in those features.
- Deleting a user while an import is running: the import is cancelled first.
- A disabled user with an active Health Auto Export automation: requests return 401 and the import history of that user records nothing.

## Acceptance criteria

- [ ] The setup page cannot be used without the token printed in the logs.
- [ ] An admin cannot reach any endpoint returning another user's health data.
- [ ] Disabling a user immediately invalidates their sessions and tokens.
- [ ] Deleting an account removes all its rows from every user-owned table.

## Future ideas

- Sharing between accounts with fine-grained scopes (per metric group, read-only).
- Storage quotas per user.
