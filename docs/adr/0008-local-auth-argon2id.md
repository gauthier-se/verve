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
