# Local accounts and sessions

- **Status:** Planned
- **Milestone:** M1 (FR-15 in M5)
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

  At most 2 passwords are hashed at once: the other logins wait for a slot, so that a burst of logins cannot exhaust the memory of a Raspberry Pi, since each hash takes 19 MiB.
- **FR-2**: Password policy: at least 12 characters, no maximum below 128, no composition rules. Common passwords are rejected with an embedded list. No external breach-checking service is called.
- **FR-3**: Login accepts username or email, in any case.

  A wrong password, an unknown login, an account that is not `active` and an account without a password all answer the same 401 `invalid_credentials`. An unknown login still verifies the password against a dummy hash with the current parameters, so that the timing does not reveal whether the account exists either. The first login since the start computes the dummy hash, whatever its login. The session is opened in one transaction with the upgraded hash, the last login time and the audit event, after reading the account again: when it changed since the verification, such as by the hash upgrade of a concurrent login or by a password change, the password is verified once more against the account as it is now.
- **FR-4**: Failed logins are rate limited per client IP and per account with increasing delays. There is no permanent lockout. Responses do not reveal whether the account exists.

  After 5 failures for one login, or 20 for one client IP, the next attempts answer 429 `rate_limited` with `Retry-After` in whole seconds, rounded up, until the delay has passed. The delay is 1 second after the failure that reaches the limit, and doubles with each further failure up to 15 minutes. A login counts whether or not an account has it, and logins that differ only by case share one count. An IPv4 client counts by its address, and an IPv6 client by its /64 network, which a single host commonly holds whole: changing its address does not get it past the limit. A success resets the count of its login, not the one of its client IP. The counts live in memory, so that a restart clears them, and a count is forgotten after an hour without a failure, counted from the end of its delay. An attempt that waited for a hashing slot (FR-1) is checked again once it has one, so that concurrent attempts cannot get past the limit.
- **FR-5**: On success, the server creates a session: a random 256-bit identifier, stored hashed, with user id, login method, creation time, last seen time, expiry, client IP and user agent.

  The identifier is 32 random bytes in unpadded base64url, returned once to the client: the database holds only the SHA-256 hash of that text. Each session also has a random public id of its own, which names it on the sessions page, so that nothing derived from the identifier reaches a URL. A lookup rejects an unknown, expired or revoked session, and a session whose user is not `active`, with the same error: callers cannot tell them apart.
- **FR-6**: The session cookie is `HttpOnly`, `SameSite=Lax` and `Path=/`. When `VERVE_PUBLIC_URL` uses HTTPS, it is `Secure` and named `__Host-verve_session`, and a cookie named `verve_session` is ignored, since another host of the domain may have set it. Otherwise, including without a public URL, it is `verve_session`, without `Secure`. Only requests under `/api/` are authenticated by the cookie: the web app and the health probes never look up a session.
- **FR-7**: Session lifetime: 30 days sliding expiration, renewed on activity, with an absolute maximum of 180 days. Both values are configurable by the admin.

  | Instance setting | Default | Purpose |
  |---|---|---|
  | `session.idle_days` | `30` | Days a session stays valid without activity |
  | `session.max_days` | `180` | Days a session stays valid at most after its creation |

  Each is a whole number from 1 to 36500 (100 years). A missing or invalid value is replaced by its default, so that a wrong value cannot lock every user out. The expiry of a session is set at login and at each renewal to the earlier of the current time plus `session.idle_days` and the creation time plus `session.max_days`, with the settings of that moment. A session is valid up to its expiry included. Idle time counts from the last renewal (FR-8), so a session renewed once and left alone for exactly 30 days is still valid. A change of the settings applies to new sessions at once, and to an existing session at its next lookup that renews it: a session idle for longer than a lowered `session.idle_days`, or older than a lowered `session.max_days`, ends there.

  The session cookie carries the expiry of the session: login sets it, and each renewal (FR-8) sends it again with the new expiry, so that the browser keeps it as long as the session lives.
- **FR-8**: `last_seen_at` is updated at most once per minute per session, together with the expiry. A lookup reads on the reader pool: only this renewal writes.
- **FR-9**: The session identifier is regenerated at login and when the user's privileges change. A login from a request that already has a session deletes that session.
- **FR-10**: Logout deletes the session server-side and clears the cookie. It answers 204, even without a session or with an unknown, expired or revoked one.
- **FR-11**: Changing the password requires the current password and revokes all other sessions.
- **FR-12**: The active sessions page lists sessions (device and browser from the user agent, approximate location is not computed, IP, login method, last seen) and allows revoking any of them or all others.
- **FR-13**: State-changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) under `/api/` must carry the custom header `Verve-Csrf: 1` and an `Origin` matching `VERVE_PUBLIC_URL`, or they answer 403 `csrf_failed`. The checks also cover unauthenticated requests, such as a login. Without a public URL, `Origin` must match the scheme and host of the request, as resolved from the `X-Forwarded-*` headers of a trusted proxy. A request without `Origin` fails. Requests that carry an API token (`Authorization: Bearer`) are exempt, and are never authenticated by their session cookie. Other methods are not checked.
- **FR-14**: Expired sessions are purged by a periodic job. The recurring job `cleanup.sessions` runs daily and removes the sessions whose stored expiry has passed. A session ended by lowered settings keeps its stored expiry, so it stays in the table until then, and lookups reject it while the lowered settings hold.
- **FR-15** (M5): Admins can disable local password login once OIDC or proxy header authentication is configured. The CLI `verve user reset-password` stays available as a break-glass path.

## UX notes

- Login page: username or email, password, link "Forgot password" only when SMTP is configured (M3). Errors are translated from their code, and `rate_limited` tells how long to wait, from `Retry-After`.
- A page that needs a session, opened without one, goes to `/login?redirect=<URL of the page>`, or to `/setup` while setup is required, and the login goes back to that URL. Only a URL of the app given by its path is accepted as the target: anything else, such as another origin, goes to the home page after login.
- When a request answers 401 during use, because the session expired or was revoked, the app goes to the login page with the current URL as the target. Logging out goes to the login page. Both clear the data that the app cached for the account.
- With a session, the header shows the navigation and a user menu with the account and logout.
- Sessions page in account settings, current session highlighted.

## Data and API notes

- `sessions` table with hashed id.
- The password policy answers with the codes `password_too_short`, `password_too_long` and `password_too_common`. Length counts characters, not bytes.
- Endpoints: `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`, `GET /api/v1/account/sessions`, `DELETE /api/v1/account/sessions/{id}`, `POST /api/v1/account/password`.
- `POST /api/v1/auth/login` takes `{"login": "...", "password": "..."}` and answers 200 with the user, as `GET /api/v1/auth/me` does, and the session cookie. The login has 1 to 254 characters, the longest email address, and the password 1 to 128, or the answer is 422 `validation_failed`, which counts no failure. A body larger than 8 KiB answers 413. `POST /api/v1/auth/logout` answers 204 with the cookie cleared (`Max-Age=0`, with the attributes of FR-6).
- `GET /api/v1/auth/me` answers the id, username, email (left out when the user has none) and role of the user of the session.
- An operation that needs a session answers 401 `unauthorized` to a request with a missing, unknown, expired or revoked session, and an operation reserved for admins answers 403 `forbidden` to a member. When the session lookup fails for another reason, such as a database error, an operation that needs a session answers 500 `internal`, and the operations without authentication, such as `GET /api/v1/meta`, answer as usual. The OpenAPI document declares the cookie as the `sessionCookie` security scheme.

## Edge cases

- Clock skew between server restarts does not matter because expiry is computed server-side.
- Behind a reverse proxy without `VERVE_TRUSTED_PROXIES`, all requests appear to come from the proxy IP, which degrades IP rate limiting: the admin status page warns about it.
- Running over plain HTTP on a LAN: cookies are not `Secure`, and the admin status page shows a warning.
- Behind a reverse proxy that rewrites the `Host` header, without `VERVE_PUBLIC_URL` and without `VERVE_TRUSTED_PROXIES`: the `Origin` of the browser no longer matches the request, so every state-changing request fails with `csrf_failed`. Setting `VERVE_PUBLIC_URL` fixes it.

## Acceptance criteria

- [ ] Password hashes in the database are argon2id with parameters stored inline.
- [ ] A revoked session is rejected on the next request.
- [ ] A cross-site form post without the CSRF header is rejected.
- [ ] Login timing and messages do not reveal whether a username exists.

## Future ideas

- Passkeys (WebAuthn) as a passwordless option.
