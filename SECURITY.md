# Security policy

Verve holds health data, which is about as intimate as personal data gets. A
flaw that exposes one account's data to another, or to the network, is treated
as the most serious kind of bug this project can have.

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Report it privately through GitHub:
[Report a vulnerability](https://github.com/gauthier-se/verve/security/advisories/new),
also reachable from the repository's Security tab. Private reporting is enabled,
so the report stays between you and the maintainer until a fix exists.

A useful report says which version or commit you tested, how the instance was
deployed, what an attacker needs (network position, an account on the instance,
a crafted export file), and the steps to reproduce. A proof of concept helps
more than a scanner output.

Verve is maintained by one person as a side project, so expect an
acknowledgement within a few days rather than within hours. You will be told
when the issue is confirmed, when a fix lands, and you will be credited in the
advisory unless you prefer otherwise. Please give the fix a reasonable window
before disclosing publicly.

## Supported versions

Verve has not reached a tagged release. Only the current `main` branch is
supported, and fixes land there. Once versions are published this section will
say which ones receive fixes.

## In scope

* Reading, writing or deleting another account's data through any endpoint.
* Authentication or session handling flaws: bypass, fixation, tokens that
  survive logout, signup reopening after bootstrap.
* Remote code execution or arbitrary file access, including anything reachable
  through a crafted Apple Health export (XML parsing, zip entry paths, GPX
  route extraction).
* Injection into the query engine or the storage layer.
* Stored or reflected cross-site scripting in the web UI, and cross-site
  request forgery against a state-changing endpoint.
* Secrets leaking into logs, error responses or the import report.

## Out of scope

* Findings that need an operator's own misconfiguration, such as running
  `--secure-cookie=false` on a public HTTPS instance, or exposing the data
  directory over a network share.
* Missing hardening headers, TLS configuration or WAF rules, all of which
  belong to the reverse proxy you put in front of Verve.
* Resource exhaustion by an authenticated account uploading large files up to
  the configured cap, which is a quota question rather than a vulnerability.
* Automated scanner output with no demonstrated impact, and dependency
  advisories with no reachable path in Verve.
* Social engineering, physical access, and anything requiring the operator to
  run attacker-supplied commands.

## What Verve does on its side

These are the guarantees a report can be measured against.

**Passwords** are hashed with argon2id at 64 MiB, 3 passes, 2 lanes, with a
random 16-byte salt, stored as a PHC string that carries its own parameters, so
costs can be raised later without invalidating existing hashes.

**Sessions** are opaque 256-bit random tokens. Only their SHA-256 digest is
stored, so a leak of the database cannot reconstruct a live cookie. The cookie
is `HttpOnly`, `SameSite=Lax`, and `Secure` by default, and it expires with the
server-side session (30 days).

**Login and account creation** share a per-IP rate limiter: a burst of five
attempts, then one attempt every twelve seconds. Failed logins return the same
response whether or not the email exists, so the endpoint does not enumerate
accounts.

**Signup closes after the first account.** The bootstrap endpoint re-checks
server-side that zero accounts exist and refuses otherwise. The public state
endpoint leaks exactly one boolean, whether the instance is initialized.

**Every read and write is scoped to the authenticated account** at the query
level, not filtered in the UI.

**Uploads** are capped (2 GiB by default, `--max-upload-mb` to change),
rejected early on `Content-Length` and again while streaming, written to a temp
directory inside the data directory, and swept at startup if a crash left them
behind.

**Verve makes no outbound network requests.** No telemetry, no update check, no
analytics, no third-party asset fetched at runtime. The SPA is embedded in the
binary.

## What is left to you

* Terminate TLS in front of Verve and keep `--secure-cookie=true`, which is the
  default. The flag exists for plain-HTTP LAN use and should not survive
  exposure to the internet.
* Treat `VERVE_DATA_DIR` as the crown jewels. It contains every reading you
  own, unencrypted at rest, plus the artifacts directory. Restrict its
  permissions and encrypt your backups.
* Create additional accounts from the CLI, and remember that Verve has no
  account recovery: a lost password is reset with
  `verve account passwd --email=...` by whoever has shell access.
