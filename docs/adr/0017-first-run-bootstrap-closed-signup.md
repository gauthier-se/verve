# First-run bootstrap account, then closed signup

## Context

ADR 0007 makes Verve multi-user with strict isolation from v1; ADR 0008 ships
local auth and creates Accounts through the CLI (`verve account create`), a fit
for an "admin-run household". Making Verve non-developer-friendly means the very
first person should not need a shell to get in — but Verve is a single binary
often exposed behind a reverse proxy, so an open web signup would let anyone on
the network create an Account and ingest data on the instance.

## Decision

The web offers an account-creation screen that is active **only while the
instance has zero Accounts** — the **bootstrap**. It creates the first Account
(the admin) and then **closes**: web signup is off, and further Accounts are
created via the CLI (`verve account create`), exactly as today. No invitation
system is built now.

The create screen collects **email + password only**. The `Me` profile (date of
birth, biological sex, blood type) stays nullable and is populated from the first
Apple import (ADR 0011), not typed by hand.

On success the endpoint **opens a session immediately** (sets the cookie like
`handleLogin`) and returns the Account — the user lands on their seeded Dashboard
(ADR 0018) with no second credential entry.

A public `GET /v1/auth/state → { needs_bootstrap }` lets the SPA pick the first
screen: create-account when `true`, login otherwise. Closure is enforced
**server-side** — the create endpoint re-checks that no Account exists and returns
a conflict if one does; the `/register` route redirects to `/login` once closed.
The endpoint leaks only one boolean ("instance initialized"), never account
enumeration, and reuses the existing per-IP login rate limiter.

## Why

- **Bootstrap, not open signup.** It removes the shell requirement for the first
  user — the whole non-dev-friendly goal — without ever exposing a standing
  public signup on a homelab instance. Once a single Account exists the surface
  is closed again.
- **CLI for later Accounts.** ADR 0008's admin-run household model already covers
  N+1 Accounts; a web invitation flow (tokens, expiry, accept screen) is a
  feature in its own right with no present need. Deferring it keeps the new
  security surface at zero.
- **Email + password only.** The `Me` profile arrives free with the first import;
  asking for it up front adds onboarding friction and would be overwritten anyway.
- **Auto-login.** Re-typing credentials chosen seconds earlier is gratuitous
  friction against the non-dev goal.
- **Server-enforced closure.** A client-only guard is not security; the endpoint
  itself must refuse once initialized. Exposing `needs_bootstrap` reveals nothing
  an attacker could not infer from the login page's existence.

## Considered Options

- **Open public signup (flag-gated, default off):** rejected — a standing signup
  surface on an exposed binary; bootstrap gives the same first-user convenience
  without it.
- **Bootstrap + web invitations now:** deferred — model an Invitation (token,
  expiry, target, revocation) plus generate/accept UI; real work for a household
  need that has not yet arrived. The CLI covers N+1 meanwhile.
- **Admin-only, no web creation at all:** rejected — leaves the first user on the
  CLI, missing the non-dev-friendly goal.
- **Collect the `Me` profile at signup:** rejected — friction, and the import
  fills it anyway.
- **Redirect to login after creation (no auto-login):** rejected — a gratuitous
  re-entry of just-chosen credentials.

## Consequences

- A new create-account handler (behind the per-IP limiter) inserts the first
  Account, re-checking emptiness server-side, then opens a session like login.
- `GET /v1/auth/state` reports `needs_bootstrap`; the SPA routes the first screen
  from it and redirects `/register` to `/login` once closed.
- The CLI `account create` / `account passwd` path is unchanged and remains the
  way to add and manage further Accounts.
- No Invitation model, table, or UI is introduced; the door for one (and for the
  forward-auth resolver seam of ADR 0008) stays open.
