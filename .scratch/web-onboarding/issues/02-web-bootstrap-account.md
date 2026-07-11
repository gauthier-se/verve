# 02 — Web first-run bootstrap account

Status: ready-for-agent
Blocked by: 01 (shared account-creation path + seeded dashboard)

## Goal

Let the first user create their Account from the browser with no shell, then
**close** web signup once an Account exists. The first Account is auto-logged-in
and lands on its seeded "Aperçu" Dashboard.

## Scope

- **Instance state.** `GET /v1/auth/state` (public) → `{ "needs_bootstrap": bool }`,
  true iff the instance has zero Accounts. Leaks only this one boolean.
- **Create endpoint** (public, behind the existing per-IP login rate limiter):
  accepts **email + password only**; **re-checks server-side** that no Account
  exists and returns a conflict if one does; creates the Account through the shared
  path from #01 (so the web Account is seeded too); opens a session (sets the
  cookie like `handleLogin`) and returns the Account — **auto-login**.
- **SPA.** Route the first screen from `needs_bootstrap`: create-account screen when
  `true`, login otherwise; `/register` redirects to `/login` once closed. Password
  input honours whatever `internal/auth/password.go` already enforces.

## Out of scope

Invitations and N+1 account creation (stay CLI). `Me` profile fields (filled at
import, ADR 0011). Web import (#03). A forward-auth resolver (ADR 0008 seam,
untouched).

## Acceptance

- [ ] Fresh instance (0 Accounts): the SPA shows the create-account screen;
      submitting email + password creates the Account, auto-logs-in, and lands on
      the seeded "Aperçu" Dashboard.
- [ ] After the first Account exists: `GET /v1/auth/state` returns
      `needs_bootstrap: false`, the create endpoint returns a conflict, the SPA
      shows login, and `/register` redirects to `/login`.
- [ ] The create endpoint is server-enforced (not client-only) and rate-limited.
- [ ] No account enumeration is possible beyond the single boolean.

## Refs

ADR 0017, ADR 0007, ADR 0008. CONTEXT.md: Bootstrap, Account.
`internal/api/authhandlers.go`, `internal/api/server.go`,
`web/src/components/login-page.tsx`, `web/src/router.tsx`,
`internal/auth/password.go`.
