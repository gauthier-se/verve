# Local accounts and sessions

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Accounts and administration](accounts-and-administration.md), [TOTP](auth-totp.md), [OIDC](auth-oidc.md), [Proxy header](auth-proxy-header.md), [ADR 0005](../decisions/0005-server-side-sessions.md)

## Summary

Username and password login with argon2id hashing and server-side sessions stored in the database. Sessions are the common result of every login method, so this feature also defines session behavior for TOTP, OIDC and proxy header logins.

## User stories

- As a user, I want to log in with a username and password that are stored safely.
- As a user, I want to see where I am logged in and log out other devices.
- As a user, I want to stay logged in on my phone without logging in every day.

## Scope

### In scope

- Password hashing, password policy, login, logout, password change.
- Session lifecycle, active sessions page.
- Rate limiting and CSRF protection.

### Out of scope

- Second factor (see [TOTP](auth-totp.md)).
- External identity (see [OIDC](auth-oidc.md) and [Proxy header](auth-proxy-header.md)).

## Functional requirements

- **FR-1**: Passwords are hashed with argon2id using parameters at or above current OWASP recommendations. Parameters are stored with each hash. When parameters are raised, hashes are upgraded at the next successful login.
- **FR-2**: Password policy: at least 12 characters, no maximum below 128, no composition rules. Common passwords are rejected with an embedded list. No external breach-checking service is called.
- **FR-3**: Login accepts username or email.
- **FR-4**: Failed logins are rate limited per client IP and per account with increasing delays. There is no permanent lockout. Responses do not reveal whether the account exists.
- **FR-5**: On success, the server creates a session: a random 256-bit identifier, stored hashed, with user id, login method, creation time, last seen time, expiry, client IP and user agent.
- **FR-6**: The session cookie is `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` and named with the `__Host-` prefix when `VERVE_PUBLIC_URL` uses HTTPS.
- **FR-7**: Session lifetime: 30 days sliding expiration, renewed on activity, with an absolute maximum of 180 days. Both values are configurable by the admin.
- **FR-8**: `last_seen_at` is updated at most once per minute per session.
- **FR-9**: The session identifier is regenerated at login and when the user's privileges change.
- **FR-10**: Logout deletes the session server-side and clears the cookie.
- **FR-11**: Changing the password requires the current password and revokes all other sessions.
- **FR-12**: The active sessions page lists sessions (device and browser from the user agent, approximate location is not computed, IP, login method, last seen) and allows revoking any of them or all others.
- **FR-13**: State-changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) authenticated by cookie must carry the custom header `Verve-Csrf: 1` and an `Origin` matching `VERVE_PUBLIC_URL`. Requests authenticated by API token are exempt.
- **FR-14**: Expired sessions are purged by a periodic job.
- **FR-15**: Admins can disable local password login once OIDC or proxy header authentication is configured. The CLI `verve user reset-password` stays available as a break-glass path.

## UX notes

- Login page: username or email, password, link "Forgot password" only when SMTP is configured.
- Sessions page in account settings, current session highlighted.

## Data and API notes

- `sessions` table with hashed id.
- Endpoints: `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`, `GET /api/v1/account/sessions`, `DELETE /api/v1/account/sessions/{id}`, `POST /api/v1/account/password`.

## Edge cases

- Clock skew between server restarts does not matter because expiry is computed server-side.
- Behind a reverse proxy without `VERVE_TRUSTED_PROXIES`, all requests appear to come from the proxy IP, which degrades IP rate limiting: the admin status page warns about it.
- Running over plain HTTP on a LAN: cookies are not `Secure`, and the admin status page shows a warning.

## Acceptance criteria

- [ ] Password hashes in the database are argon2id with parameters stored inline.
- [ ] A revoked session is rejected on the next request.
- [ ] A cross-site form post without the CSRF header is rejected.
- [ ] Login timing and messages do not reveal whether a username exists.

## Future ideas

- Passkeys (WebAuthn) as a passwordless option.
