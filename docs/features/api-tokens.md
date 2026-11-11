# API tokens

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Health Auto Export endpoint](ingestion-health-auto-export.md), [Public API](public-api.md), [Accounts and administration](accounts-and-administration.md)

## Summary

Personal, scoped Bearer tokens for machine clients: Health Auto Export, the future iOS companion app, scripts, Home Assistant. Tokens are separate from browser sessions and revocable at any time.

## User stories

- As a user, I want a token that can only send data, to paste into Health Auto Export.
- As a user, I want a read-only token for a Home Assistant sensor.
- As a user, I want to see when a token was last used and revoke it.

## Scope

### In scope

- Token creation, listing, revocation, optional expiry.
- Scopes `ingest` and `read`.
- Usage metadata.

### Out of scope

- Write scopes for plans, dashboards or settings.
- Tokens with admin privileges.
- OAuth flows for third-party apps.

## Functional requirements

- **FR-1**: A token has a name, one or more scopes, an optional expiry date, a creation date, a last used date and a last used IP.
- **FR-2**: Scopes:
  - `ingest`: ingestion endpoints only.
  - `read`: read-only endpoints of the public API for the token owner's data.
- **FR-3**: Token format: `vrv_` followed by at least 32 random bytes encoded in base62. The full token is shown once at creation. Only a SHA-256 hash and a short display prefix (for example `vrv_3fK9`) are stored.
- **FR-4**: Tokens are sent as `Authorization: Bearer <token>`. Tokens in query strings are rejected.
- **FR-5**: A request with a valid token for a disabled user, a revoked token or an expired token returns 401 with a problem `code` that distinguishes the cases for the owner's diagnosis (the audit log records which).
- **FR-6**: A request with a valid token but a missing scope returns 403.
- **FR-7**: Last used date and IP are updated at most once per minute per token.
- **FR-8**: Users manage only their own tokens. Admins see the number of tokens per user, not the tokens.
- **FR-9**: Each token is rate limited (generous defaults suitable for scheduled syncs), returning 429 with `Retry-After`.
- **FR-10**: The token creation dialog offers presets: "Health Auto Export" (scope `ingest`), "Read only" (scope `read`), "Custom".
- **FR-11**: After creating a Health Auto Export token, the dialog shows the exact endpoint URL and header to configure in the app.

## UX notes

- Tokens page in account settings: table with name, scopes, prefix, created, last used, expiry, and a revoke action.
- Warning when a token has not been used for 90 days, suggesting revocation.

## Data and API notes

- `api_tokens` table. A token is `vrv_` followed by 43 base62 characters (256 bits), each drawn from `crypto/rand` by rejection sampling, so that the alphabet carries no modulo bias. The display prefix is its first 8 characters, such as `vrv_3fK9`.
- The name of a token is not empty, and its scopes are one or more distinct known scopes. The scopes keep the order they were given in.
- A revoked token stays, with its revocation time, so that FR-5 can tell it apart from an unknown token. A second revocation keeps the first time. Account deletion removes the tokens of the user. The repository returns revoked and expired tokens, and the tokens of disabled users: the authentication decides.
- Endpoints, for the user of a session only. A request authenticated by a token answers 401 `unauthorized`: a token cannot create, list or revoke tokens.
  - `POST /api/v1/account/tokens` takes `{"name": "...", "scopes": ["ingest", "read"], "expiresAt": "..."}`, the expiry being optional, and answers 201 with the token record and the full `token`, once. The name has 1 to 64 characters. An unknown, missing or repeated scope, or a name out of bounds, answers 422 `validation_failed`, and an expiry that is not after the current time answers 422 `expiry_in_past`. A body larger than 4 KiB answers 413.
  - `GET /api/v1/account/tokens` lists the tokens of the user, revoked ones included, newest first. Each token has `id`, `name`, `scopes`, `prefix`, `createdAt`, and `expiresAt`, `lastUsedAt`, `lastUsedIp` and `revokedAt`, each left out when unset. Never the token or its hash.
  - `DELETE /api/v1/account/tokens/{id}` revokes the token and answers 204: the next request with it answers 401 `token_revoked`. Revoking a revoked token answers 204 and changes nothing. The token of another user, or an unknown id, answers 404 `not_found`.
  - The creation and the revocation record `token.created` and `token.revoked`, see [Audit log](audit-log.md). A second revocation records nothing.
- `GET /api/v1/admin/users` gives each user `activeTokens`, the number of their tokens that are neither revoked nor expired, and never the tokens (FR-8).
- Rate limit (FR-9): each token gets 60 requests per minute, with bursts of 120, enough for scheduled syncs that send batches. A request above the limit answers 429 `rate_limited` with `Retry-After` in whole seconds, rounded up, before reaching its operation, and is not counted. The limit applies to valid tokens only, after their lookup: a rejected token answers 401 as before. The counters are kept in memory per token, so a restart clears them, and a token quiet for 2 minutes, which has its whole burst back, is forgotten.
- Authentication, in the middleware that also reads the session cookie:
  - A request with `Authorization: Bearer` is authenticated by its token alone, never by its cookie. The scheme is case-insensitive, and the spaces around the token and a second `Bearer ` prefix are dropped (Edge cases).
  - The principal of a token has its user, the token id, its scopes and the method `token`, but no role: a token never carries the privileges of an admin.
  - A token that does not authenticate answers 401 on any operation under `/api/`, also on those that need no authentication, so that the client learns that its credential no longer works. The codes, in this order of precedence: `token_revoked`, `token_expired` (a token is valid until its expiry included), `user_disabled` (also for an account being deleted), and `unauthorized` for an unknown token, which cannot be told apart from a mistyped one.
  - A query string with the parameter `access_token` (RFC 6750), in any case and even empty, or with a parameter or a value that has the shape of a token (`vrv_` and 43 base62 characters), answers 400 `token_in_query` on any path under `/api/`, whatever the method and the `Authorization` header, before the CSRF checks. A text that only starts with `vrv_`, such as a search, goes on. The token is never looked up, so its last use is not written.
  - Operations accept sessions only by default, and answer 401 `unauthorized` to a token. An operation of the public API or of the ingestion accepts a session or a token with its scope, and answers 403 `scope_missing` to a token without it. The OpenAPI document declares the `bearerToken` security scheme.
  - The last used date and IP are written by the first request after a minute without a write (FR-7). A request without a known client IP writes an empty IP.
  - A rejected known token records `token.rejected`, see [Audit log](audit-log.md). An unknown token records nothing: it names no user.
  - A token that cannot be checked, because its lookup fails or because the audit event of its rejection cannot be recorded, answers 500 `internal` on any operation. Unlike a cookie, a token is sent on purpose: the request never goes on as if it were fine.

## Edge cases

- Token pasted with surrounding spaces or a `Bearer ` prefix in the app's header value field: trimmed and accepted when unambiguous.
- Two automations sharing one token: allowed, the import history shows the automation name from Health Auto Export headers.

## Acceptance criteria

- [ ] The full token is never retrievable after creation.
- [ ] An `ingest` token cannot read data.
- [ ] A `read` token cannot ingest data.
- [ ] Revocation takes effect on the next request.

## Future ideas

- QR code pairing for the iOS companion app (server URL plus a freshly created `ingest` token).
- Write scopes if the companion app needs them.
