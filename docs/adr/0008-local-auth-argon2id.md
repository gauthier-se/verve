# Built-in local auth (argon2id), extensible to forward-auth

## Context

Verve is multi-user from v1 and ships as a single self-contained binary for
homelab use. Homelabbers often front apps with an SSO (Authelia, Authentik,
Tailscale) via reverse-proxy forward-auth, but requiring that infrastructure
would break "works standalone".

## Decision

Ship **built-in local authentication** in v1: username/password hashed with
**argon2id**, signed cookie **sessions**. Structure the auth middleware so a
**forward-auth** mode (trusting a reverse-proxy identity header) can be added
later without reworking it. Import stays a host-side CLI operation that names its
target Account (`verve import --account=… export.zip`); self-service resumable
**web upload** for the 747 MB export comes in v1.x.

## Why

Local auth keeps the single binary fully self-contained with no external
dependency. Deferring SSO (as an additive mode) avoids imposing infra on users
who just want to run the binary, while not closing the door for those who want
SSO. CLI-with-`--account` fits an admin-run household at first; web upload
follows for community self-service.

## Sessions: server-side records, not stateless tokens

Sessions are **server-side records** (an `auth_sessions` table) keyed by the
SHA-256 of a 256-bit opaque token; the raw token lives only in the cookie. This
is chosen over a signed stateless token (e.g. a JWT) because:

- **Logout is real revocation.** Deleting the row kills the session immediately,
  with no window where a still-valid signed token keeps working.
- **No signing-key lifecycle.** A stateless token needs a secret to sign and
  rotate; a random token looked up in a table needs neither, and a table leak
  cannot forge cookies (only hashes are stored).
- **Cost is negligible.** The self-hosted scale is tiny and the lookup is a
  single indexed SQLite read on a request that already touches the DB.

The token is opaque and high-entropy, so it is not additionally signed — signing
would add key management for no gain. The cookie is `HttpOnly`, `SameSite=Lax`,
and `Secure` in production (relaxable for plain-HTTP local dev).

## Middleware seam for forward-auth

`authenticate` resolves identity through an `authResolver` interface; v1 ships
only the cookie-session resolver. A forward-auth resolver (trusting a
reverse-proxy identity header) can be added and swapped in later with no change
to the middleware or the handlers, which only read the resolved Account from the
request context.
