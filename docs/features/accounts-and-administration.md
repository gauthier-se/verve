# Accounts and administration

- **Status:** Planned
- **Milestone:** M1 (FR-7 in M3)
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

- **FR-1**: When no user exists, `serve` prints a setup token at startup. The `/setup` page requires this token to create the first admin. This prevents someone else on the network from claiming the instance first.
  - Output: one dedicated line on stderr, where `serve` writes its logs, containing the link `<VERVE_PUBLIC_URL>/setup#<token>`, or the path `/setup#<token>` to append to the server address when `VERVE_PUBLIC_URL` is not set. The line is written directly to stderr, outside the `slog` handler: it is plain text in both log formats, printed whatever `VERVE_LOG_LEVEL`, and the token is never a log attribute. Under a key that the redaction of [Observability](platform-observability.md) FR-4 hides, such as `setup_token`, it would be written as `[REDACTED]`. This line is the only exception to the rule that logs never contain tokens.

    The line is written once the migrations are applied, before the server is ready:

    ```text
    Create the first admin at https://verve.example.com/setup#<token>
    Create the first admin at /setup#<token> on the address of this server
    ```
  - Validity: the token is random (256 bits), held in memory only and never stored: the server keeps only its SHA-256 hash, and compares the hash of a submitted token with it in constant time. It stops being valid as soon as a user exists, whether created through `/setup` or with `verve user create --admin` (FR-2), and when the process stops. A setup request that finds a user, or creates one, drops the token from memory, so it does not become valid again if no user is left later. Each start while no user exists generates and prints a new token. Once a user exists, `serve` prints no setup line.
  - Use: the link carries the token in the URL fragment, never in the path or query string, and the SPA handles it like the invitation link of FR-4. When the fragment is empty, the page asks for the token.
- **FR-2**: Alternatively, the first admin can be created with `verve user create --admin <username>`, which disables the setup page. The command works while `serve` runs: it opens the database in shared mode (see [Single binary](platform-single-binary.md) FR-7). On a terminal, it asks the password twice without echo and fails with `passwords do not match` when they differ. With `--password-stdin`, it reads the password from stdin instead, without its final newline. Without a terminal or `--password-stdin`, it fails and says to pass the flag. It applies the username rules and the password policy of [Local accounts and sessions](auth-local-accounts-and-sessions.md) FR-2, with a message saying what to change. Without `--admin`, it creates a member.
- **FR-3**: Roles: `admin` and `member`. Admins have all member capabilities for their own data plus administration.
- **FR-4**: Admins invite users by creating an invitation (username, optional email, role). Verve produces a single-use invitation link valid for 72 hours, which the admin copies or sends by email when SMTP is configured. The invitee chooses their own password through the link. The link carries its token in the URL fragment, never in the path or query string: `<VERVE_PUBLIC_URL>/invite#<token>`, for example `https://verve.example.com/invite#<token>`. The SPA reads the token from the fragment and removes the fragment from the address bar with `history.replaceState` right away, before sending any request, so that the token does not stay in the address bar or in the history entry. It keeps the token in memory only (component state, never `sessionStorage` or `localStorage`) and sends it to the API in a request body (see the security baseline in [Architecture](../architecture.md#security-baseline)).
- **FR-5**: Admins can disable and re-enable accounts. Disabling revokes all sessions and makes all API tokens of that user fail until re-enabled.
- **FR-6**: Admins can generate a single-use password reset link for a user (valid 24 hours). The admin never sets or sees the password. Like the invitation link, it carries its token in the URL fragment, `<VERVE_PUBLIC_URL>/reset-password#<token>`, and the SPA handles it as in FR-4: it removes the fragment from the address bar with `history.replaceState` right after reading the token, before sending any request, keeps the token in memory only and sends it to the API in a request body.
- **FR-7** (M3): Self-service password reset by email is available when SMTP is configured (see [Notifications](notifications.md)). The emailed link is a password reset link as described in FR-6.
- **FR-8**: Admins can promote and demote users. The last active admin cannot be demoted, disabled or deleted.
- **FR-9**: The admin user list shows metadata only: username, email, role, status, created date, last login, number of samples, storage used, last ingestion per origin. No health values, charts or plans of other users.
- **FR-10**: The storage layer exposes no operation that reads another user's health data, so the restriction does not rely on the UI alone.
- **FR-11**: A user can delete their own account after re-entering their password (or re-authenticating through OIDC). Deletion removes all their data: samples, workouts, aggregates, plans, dashboards, tokens, sessions, uploads, exports, notifications settings. Audit events about the deletion are kept without health content.
- **FR-12**: An admin can delete another user's account with a confirmation that requires typing the username.
- **FR-13**: Account deletion runs as a background job and reports completion. The account is disabled immediately.
- **FR-14**: The instance settings page groups admin-managed settings: general (public URL warning, default language), map, SMTP, backups, OIDC, proxy header, 2FA enforcement. A group appears with the feature it configures.

## UX notes

- Setup wizard: setup token, admin account, timezone, then a short guide to importing data. Default language and units join it in M2, with [User preferences and profile](user-preferences-and-profile.md).
  - The token step appears when the link gave no token. It accepts the token or the whole link, as the server log prints it. When the server answers `setup_token_invalid`, the wizard comes back to this step with the message, and keeps what the user entered for the admin account.
  - The admin account step asks the password twice, and checks that both match before sending the request. Its errors are translated from their code, and `setup_completed` links to the login page.
  - The timezone step proposes the timezone of the browser, so that the first login does not ask for it again.
  - The steps and the token live in the memory of the page, never in `localStorage`, `sessionStorage` or the history state. A reload before the admin exists starts again from the token step, and a reload after it goes to the login page.
- Admin area is a separate section in navigation, visible only to admins. Its pages are under `/admin`, and a member who opens one gets the not-found page.
- Deleting data is always behind a confirmation dialog describing exactly what will be removed.

## Data and API notes

- `users` (username unique, email optional and unique when present, role, status `active`, `disabled` or `deleting`). `password_hash` is empty for the external logins of M5.
- Usernames have 1 to 64 characters, without `@`, whitespace, control characters or invisible format characters such as a zero-width space, or the code is `username_invalid`. Emails contain `@`. A login is therefore never ambiguous between a username and an email. Lengths count characters, not bytes.
- Usernames and emails keep the case they were created with, and are unique and looked up ignoring case, in every alphabet, through the columns `username_fold` and `email_fold` ([ADR 0013](../decisions/0013-case-folded-logins.md)).
- `invitations` (kind `invite` or `reset`, token hash, username, email, role, user, expiry, used at, created by). The token is 256 random bits, and only its SHA-256 hash is stored.
- Invitation endpoints:
  - `POST /api/v1/admin/invitations`, for admins only (403 `forbidden` for a member), takes `{"username": "...", "email": "...", "role": "admin" | "member"}`, the email being optional, and answers 201 with the invitation and its `link`, once: `<VERVE_PUBLIC_URL>/invite#<token>`, or `/invite#<token>` when `VERVE_PUBLIC_URL` is not set, to which the client prepends its origin. A username or email that a user has, ignoring case, answers 409 `username_taken` or `email_taken`. A username that breaks the rules answers 422 `username_invalid`, and an email without `@` answers 422 `email_invalid`. Until the admin screen of M1-E12, invitations are created through the API.
  - `POST /api/v1/invitations/inspect`, without authentication, takes `{"token": "..."}` and answers the `username` and `expiresAt` of the invitation.
  - `POST /api/v1/invitations/accept`, without authentication, takes `{"token": "...", "password": "..."}`, creates an active user with the username, email and role of the invitation, marks it used, opens their session and answers 201 with the user, as `GET /api/v1/auth/me` does, and the session cookie. A password that breaks the policy answers 422 with the code of the rule, and the invitation stays valid. A username or email that a user took since the invitation answers 409 `username_taken` or `email_taken`. The invitation is checked again in the transaction that creates the user, so that of two concurrent accepts, only one creates them.
  - Both answer 410 `invitation_invalid` for an invitation used, expired or unknown. An invitation is valid until its expiry, included. A body larger than 8 KiB answers 413.
- The invitation page, `/invite`, reads the token from the fragment as FR-4 describes, inspects the invitation, names the invited username and asks the password twice. A link without a token, or whose invitation is no longer valid, shows a message that says so and links to the login page. Once the invitation is accepted, the app opens with the session of the new account.
- Password reset endpoints:
  - `POST /api/v1/admin/users/{id}/password-reset`, for admins only (403 `forbidden` for a member), takes no body and answers 201 with the `userId`, the `expiresAt` and the `link` of a reset link valid 24 hours, once: `<VERVE_PUBLIC_URL>/reset-password#<token>`, or `/reset-password#<token>` when `VERVE_PUBLIC_URL` is not set. The reset link is a row of `invitations` with kind `reset` and the user. Creating one deletes the unused reset links of the user, so that only the latest works. An unknown user answers 404 `not_found`, and an account being deleted 409 `user_deleting`. Until the admin screen of M1-E12, reset links are created through the API.
  - `POST /api/v1/password-reset`, without authentication, takes `{"token": "...", "password": "..."}`, replaces the password, marks the link used and revokes every session of the user, and answers 204. It opens no session: the user logs in with the new password. A password that breaks the policy answers 422 with the code of the rule, and the link stays valid. A link used, expired, unknown, replaced by a newer one or of an account being deleted answers 410 `reset_link_invalid`. A link is valid until its expiry, included. The link is checked again in the transaction that replaces the password, so that of two concurrent resets, only one succeeds. A body larger than 8 KiB answers 413.
- The password reset page, `/reset-password`, reads the token from the fragment as FR-6 describes and asks the new password twice. A link without a token, or whose reset answers `reset_link_invalid`, shows a message that says so and links to the login page. Once the password is replaced, the page goes to the login page, which says that the password changed.
- Admin endpoints are under `/api/v1/admin/...` and require the admin role.
- Account management endpoints, for admins only (403 `forbidden` for a member):
  - `GET /api/v1/admin/users` lists the users newest first by creation, paginated as described in [Architecture](../architecture.md#pagination). Each user has `id`, `username`, `email` (left out when there is none), `role`, `status`, `createdAt` and `lastLoginAt` (left out when they never logged in), and nothing about their health (FR-9, FR-10). The number of samples, the storage used and the last ingestion per origin join the list in M1-E7.
  - `PUT /api/v1/admin/users/{id}/status` takes `{"status": "active" | "disabled"}`, and `PUT /api/v1/admin/users/{id}/role` takes `{"role": "admin" | "member"}`. Both answer 200 with the user, as the list shows them. Disabling revokes every session of the user at once, and their logins fail until they are enabled again (FR-5). A role change revokes every session of the user, so that they log in again with a new session ([Local accounts and sessions](auth-local-accounts-and-sessions.md) FR-9). A status or role that the user already has changes nothing and records nothing.
  - Demoting or disabling the last active admin answers 409 `last_admin` (FR-8). The check runs in the transaction of the write, so that of two concurrent demotions of the last two admins, one fails. A change of an account being deleted answers 409 `user_deleting`, so that it does not cancel the deletion. An unknown user answers 404 `not_found`.
- Account deletion endpoints (FR-11 to FR-13):
  - `DELETE /api/v1/account` takes `{"password": "..."}`, the password of the user of the session, and answers 202 with the session cookie cleared. A wrong password answers 403 `invalid_credentials` and counts as a failed login of the user, which delays their attempts with 429 `rate_limited` as the login does. Re-authentication through OIDC comes with M5.
  - `DELETE /api/v1/admin/users/{id}`, for admins only (403 `forbidden` for a member), takes `{"confirmation": "..."}`, which must be the exact username of the user, case included, or the request answers 422 `confirmation_mismatch`. It answers 202 with the user, as the list shows them, with the status `deleting`. An admin who deletes their own account also gets the session cookie cleared. An unknown user answers 404 `not_found`, and an account whose deletion job is queued or running 409 `user_deleting`. Once that job has failed for good, after its last attempt, the account stays `deleting` and a new request by an admin queues the job again, recorded as a new request.
  - Both answer 409 `last_admin` for the last active admin (FR-8), checked in the transaction of the request. In that transaction, the account takes the status `deleting`, every session of the user is revoked, their queued jobs are cancelled and the cancellation of their running jobs is requested, and the job `account.delete` is queued. A user being deleted cannot log in, and their username and email stay taken until the job has finished.
  - The job `account.delete` carries the user in its payload, `{"userId": 7}`, and has no user of its own, so that it does not remove itself. Its serial key, `account.delete:<user id>`, keeps an account to one deletion job at a time. It cancels the jobs of the user again and, while one of them still runs, fails with an error and retries later, up to 10 attempts, without removing anything. It then removes the user in one transaction, whose foreign keys remove their sessions, preferences and links, and records the completion, and last removes the jobs of the user, which have no foreign key, in batches of at most 1,000 per transaction. The large tables of later milestones are removed in batches before the user. The job is safe to run twice: a run that finds the user gone only removes their remaining jobs. A user who is not being deleted fails the job at once.
  - A test lists every table of the schema with a `user_id` column, seeds a row of two users in each, deletes one of them and fails when a table keeps a row of the deleted user or has no seed, so that a new table cannot be forgotten.
- The command line manages the users with the same rules, also while `serve` runs, in shared mode: `verve user list` prints the users with the metadata of the list, `verve user disable <username>` and `verve user enable <username>` change their status, and `verve user reset-password [--password-stdin] <username>` reads the new password as `user create` does (FR-2), applies the password policy and revokes the sessions of the user.
- Setup endpoints, without authentication:
  - `GET /api/v1/setup` answers `{"required": true}` while no user exists, and `{"required": false}` once one does. The count is read at each request, so a user created by the CLI while the server runs is seen at once.
  - `POST /api/v1/setup` takes `{"token": "...", "username": "...", "password": "..."}`, creates an active admin, opens their session and answers 201 with the user, as `GET /api/v1/auth/me` does, and the session cookie. Once a user exists, it answers 409 `setup_completed` whatever the token. Otherwise a wrong token answers 403 `setup_token_invalid`, and a username or password that breaks the rules answers 422 `username_invalid`, `password_too_short`, `password_too_long` or `password_too_common`. A body larger than 8 KiB answers 413. The count and the token are checked before the transaction, so a rejected request never takes the writer or hashes a password. The count is read again in the transaction that creates the admin, so that of two concurrent setups, or of a setup and `verve user create`, only one creates a user. It is also read again after a wrong token: a setup overtaken by another one, which created the admin and revoked the token, answers 409 too.
- Each creation records the audit event `user.created`, each invitation `user.invited`, and each change of status, role or password `user.disabled`, `user.enabled`, `user.role_changed` or `user.password_reset`, each reset link `user.password_reset_link_created`, each reset through a link `auth.password_reset_completed`, and each account deletion `user.deletion_requested` and `user.deletion_completed` (see [Audit log](audit-log.md)).

## Edge cases

- Setup token lost, for example after log rotation: restarting Verve prints a new one and invalidates the old one.
- Invitation link used twice: second use fails with a clear message.
- Username collision with an OIDC or proxy header identity: handled by the linking rules in those features.
- Deleting a user while an import is running: the import is cancelled first.
- A disabled user with an active Health Auto Export automation: requests return 401 and the import history of that user records nothing.

## Acceptance criteria

- [ ] The setup page cannot be used without the token printed at startup.
- [ ] The setup token appears only in the dedicated startup line, never in a log attribute.
- [ ] Once a user exists, the setup token is rejected and `serve` prints no setup line.
- [ ] An admin cannot reach any endpoint returning another user's health data.
- [ ] Disabling a user immediately invalidates their sessions and tokens.
- [ ] Deleting an account removes all its rows from every user-owned table.

## Future ideas

- Sharing between accounts with fine-grained scopes (per metric group, read-only).
- Storage quotas per user.
