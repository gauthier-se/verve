# Trusted proxy header authentication

- **Status:** Planned
- **Milestone:** M5
- **Related:** [OIDC](auth-oidc.md), [Local accounts and sessions](auth-local-accounts-and-sessions.md), [Configuration and deployment](platform-configuration-and-deployment.md)

## Summary

When Verve sits behind a reverse proxy that authenticates users with forward-auth (Authelia, Authentik outpost, oauth2-proxy), the proxy sends the user identity in headers such as `Remote-User`. Verve can trust these headers, but only for requests coming from configured proxy addresses. The feature is off by default because a misconfiguration would let anyone forge an identity.

## User stories

- As a homelabber with forward-auth in front of all my services, I want Verve to recognize me without a second login.
- As an admin, I want Verve to refuse forged headers from anything other than my proxy.

## Scope

### In scope

- Header-based identity from trusted proxies, user matching, optional auto-provisioning and group mapping, logout redirect.

### Out of scope

- Using headers for API token clients.

## Functional requirements

- **FR-1**: Configuration: enabled flag (default off), user header (default `Remote-User`), optional email header (`Remote-Email`), name header (`Remote-Name`) and groups header (`Remote-Groups`), trusted proxy CIDRs (required), logout URL of the proxy.
- **FR-2**: Verve refuses to enable the feature when the trusted proxy list is empty.
- **FR-3**: Headers are read only when the TCP peer address is in the trusted list. From any other address, these headers are stripped and ignored.
- **FR-4**: User matching: by username, then by email when the email header is present and matches exactly one user. Unknown users are refused unless auto-provisioning is enabled with an allowed group (same rules as [OIDC](auth-oidc.md) FR-7 and FR-8).
- **FR-5**: On the first authenticated request, Verve creates a server-side session with login method `proxy`. On subsequent requests, if the header identity differs from the session user, the session is revoked and a new one is created for the header identity. If the header disappears, the session is revoked.
- **FR-6**: Logout revokes the session and redirects to the configured proxy logout URL.
- **FR-7**: Disabled Verve accounts are refused.
- **FR-8**: The documentation insists that ingestion endpoints (`/api/v1/ingest/*`) must be excluded from forward-auth at the proxy, because Health Auto Export and the companion app authenticate with tokens and cannot pass forward-auth. Examples are given for Traefik, Caddy and Nginx.
- **FR-9**: The admin status page shows a warning when proxy header authentication is on and requests are reaching Verve from untrusted addresses.

## UX notes

- Admin page with a "Check headers" diagnostic showing which headers Verve sees for the current request and whether the peer is trusted.

## Data and API notes

- No new table. Sessions record login method `proxy`.

## Edge cases

- Proxy and Verve in the same Docker network with dynamic IPs: the documentation recommends trusting the Docker network CIDR.
- Header case variations: header names are case-insensitive.
- Multiple values in the groups header: comma separated, trimmed.

## Acceptance criteria

- [ ] A request with `Remote-User` from an untrusted address is not authenticated.
- [ ] A request with `Remote-User` from a trusted proxy logs in the matching user.
- [ ] Changing the header identity switches the session to the new user.

## Future ideas

- Trusting a signed JWT header from the proxy instead of plain headers.
