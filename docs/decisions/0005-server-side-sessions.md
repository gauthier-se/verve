# 0005. Server-side sessions for every login method

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Verve supports four login methods: local accounts, TOTP as a second factor, OIDC and a trusted proxy header. It must support immediate revocation (logout, active sessions page, disabled accounts) and keep tokens out of reach of JavaScript.

## Decision

- Every successful login, whatever the method, creates a row in a `sessions` table. The browser receives a random 256-bit identifier in an `HttpOnly`, `Secure`, `SameSite=Lax` cookie. Only a hash of the identifier is stored.
- No JWT for browser sessions.
- Machine clients (Health Auto Export, scripts, Home Assistant) use separate API tokens sent as `Authorization: Bearer`, with scopes.
- CSRF protection relies on `SameSite`, a required custom header on state-changing requests and an `Origin` check.

## Consequences

- Revocation is immediate and simple.
- Each request needs a session lookup, which is cheap with SQLite and an index. `last_seen_at` updates are throttled to avoid write amplification.
- A stolen database does not reveal usable session ids or tokens.

## Alternatives considered

- **JWT in cookies or local storage**: stateless but hard to revoke, and local storage is exposed to XSS.
- **Reuse API tokens for the SPA**: mixes two threat models and exposes long-lived secrets to JavaScript.
