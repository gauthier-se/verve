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

- `api_tokens` table.
- Endpoints: `GET /api/v1/account/tokens`, `POST /api/v1/account/tokens`, `DELETE /api/v1/account/tokens/{id}`.

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
