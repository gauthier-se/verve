# TOTP two-factor authentication

- **Status:** Planned
- **Milestone:** M5
- **Related:** [Local accounts and sessions](auth-local-accounts-and-sessions.md), [Audit log](audit-log.md), [Accounts and administration](accounts-and-administration.md)

## Summary

Local accounts can enable a second factor with time-based one-time passwords (RFC 6238) from any authenticator app, with single-use recovery codes. Admins can require it for every local account. OIDC and proxy header logins rely on the identity provider for second factors.

## User stories

- As a user exposing Verve outside my LAN, I want a second factor on my account.
- As an admin, I want to require 2FA for all local accounts.
- As a user who lost their phone, I want recovery codes to get back in.

## Scope

### In scope

- Enrollment, verification at login, recovery codes, disabling, admin reset, enforcement.

### Out of scope

- WebAuthn and passkeys (future), SMS or email codes.
- Second factor for API tokens.

## Functional requirements

- **FR-1**: Enrollment: Verve generates a secret, shows a QR code and the manual key, and activates 2FA only after the user enters a valid code.
- **FR-2**: Parameters: SHA-1, 6 digits, 30 second period (widest authenticator compatibility), one period of tolerance before and after.
- **FR-3**: Secrets are encrypted at rest with the instance secret key.
- **FR-4**: A code cannot be reused within its validity window.
- **FR-5**: At enrollment, 10 recovery codes are generated, shown once, stored hashed, each usable once. Users can regenerate them (invalidating the previous set) after re-authenticating.
- **FR-6**: Login flow: after a valid password, the session is created only after a valid TOTP or recovery code. The intermediate state expires after 5 minutes. Attempts are rate limited.
- **FR-7**: Disabling 2FA requires the password and a valid code.
- **FR-8**: Admins can reset 2FA for a user who lost access (audited). The user must re-enroll at next login if enforcement is on.
- **FR-9**: Enforcement: when the admin enables "Require 2FA for local accounts", users without 2FA are redirected to enrollment after login and cannot use the app until enrolled.
- **FR-10**: Using a recovery code emits an audit event and shows the number of remaining codes.

## UX notes

- Security settings page: status, enable or disable, regenerate recovery codes, with a download or copy option for codes.

## Data and API notes

- `totp_secrets`, `recovery_codes` tables.
- Endpoints: `POST /api/v1/account/totp/setup`, `POST /api/v1/account/totp/verify`, `DELETE /api/v1/account/totp`, `POST /api/v1/account/totp/recovery-codes`, `POST /api/v1/auth/login/totp`.

## Edge cases

- Server clock drift: tolerance window covers small drift. `/readyz` does not check time, but the admin status page warns when the server clock looks wrong compared to client time.
- Lost secret key: TOTP secrets cannot be decrypted; affected users must be reset by an admin.

## Acceptance criteria

- [ ] A user with 2FA cannot obtain a session with the password alone.
- [ ] Each recovery code works exactly once.
- [ ] With enforcement on, a user without 2FA cannot reach any page except enrollment.

## Future ideas

- Passkeys (WebAuthn) as a second factor or passwordless login.
