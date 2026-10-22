# OIDC

- **Status:** Planned
- **Milestone:** M5
- **Related:** [Local accounts and sessions](auth-local-accounts-and-sessions.md), [Accounts and administration](accounts-and-administration.md), [Proxy header](auth-proxy-header.md), [ADR 0005](../decisions/0005-server-side-sessions.md)

## Summary

Users can sign in with the household identity provider (Authentik, Authelia, Pocket ID, Keycloak, Zitadel...) using OpenID Connect. OIDC identities are linked to local Verve accounts. By default no account is created automatically, which preserves the invitation-only model. Optionally, accounts can be provisioned automatically for members of a given group.

## User stories

- As a homelabber, I want to log into Verve with the same account as my other services.
- As an admin, I want family members in the `verve-users` group to get an account on first login.
- As an admin, I want members of `verve-admins` to be Verve admins.

## Scope

### In scope

- One OIDC provider.
- Authorization Code flow with PKCE.
- Account linking by verified email or from account settings.
- Optional auto-provisioning and role mapping by groups.
- Option to disable local password login.

### Out of scope

- Multiple providers, RP-initiated logout, back-channel logout (future).
- SAML.

## Functional requirements

- **FR-1**: Configuration (environment variables or admin UI): issuer URL, client ID, client secret, scopes (default `openid profile email`, plus `groups` when group features are used), button label (for example "Sign in with Authentik"), username claim (default `preferred_username`), groups claim (default `groups`).
- **FR-2**: Redirect URI: `<VERVE_PUBLIC_URL>/api/v1/auth/oidc/callback`, displayed in the admin UI for copying into the provider.
- **FR-3**: Flow: Authorization Code with PKCE, `state` and `nonce` checks, ID token signature verified with the provider's discovered keys, issuer and audience checks.
- **FR-4**: Identity stored as `(issuer, subject)` linked to a user.
- **FR-5**: Linking at first OIDC login:
  - if the identity is already linked: log in,
  - else if the `email` claim is present, `email_verified` is true and matches exactly one local user's email: link and log in,
  - else if auto-provisioning is enabled and the user is in the allowed group: create the account and log in,
  - else: refuse with a message asking the admin for an invitation.
- **FR-6**: Users can also link or unlink an OIDC identity from their account settings while logged in. Unlinking is refused if it would leave the account with no login method.
- **FR-7**: Auto-provisioning (off by default): requires an allowed group. The new account uses the username claim (de-duplicated if taken), email and display name from claims.
- **FR-8**: Role mapping (off by default): an admin group grants the admin role, evaluated at each login. The last admin is never demoted by mapping; the conflict is logged and shown to admins.
- **FR-9**: Successful OIDC logins create a standard server-side session with login method `oidc`. TOTP is not asked, the provider is responsible for second factors.
- **FR-10**: When "Disable local password login" is on, the login page shows only the OIDC button. The CLI password reset remains as break-glass.
- **FR-11**: Disabled Verve accounts cannot log in through OIDC.

## UX notes

- Login page shows the OIDC button above the local form, or alone when local login is disabled.
- Admin OIDC page with a "Test configuration" action that runs discovery and reports issues.

## Data and API notes

- `oidc_identities` table. Client secret encrypted at rest.
- Endpoints: `GET /api/v1/auth/oidc/start`, `GET /api/v1/auth/oidc/callback`, `POST/DELETE /api/v1/account/oidc`.

## Edge cases

- Provider unreachable at startup: Verve starts, the OIDC button shows an error when used, local login still works if enabled.
- Email changed at the provider: login still works through the linked subject.
- Two local users with the same email: automatic linking by email is refused, manual linking required.

## Acceptance criteria

- [ ] Login works with Authentik, Authelia and Pocket ID test instances.
- [ ] Without auto-provisioning, an unknown identity cannot create an account.
- [ ] A user removed from the admin group loses the admin role at next login, unless they are the last admin.

## Future ideas

- Multiple providers.
- RP-initiated logout.
