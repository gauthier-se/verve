# 05 — Local argon2id auth + sessions + Account scoping

Status: ready-for-agent
Blocked by: 01, 04

## Goal

Turn the multi-user model into real, enforced isolation: local login, sessions,
and Account-scoped access on every API request.

## Scope

- **Password hashing** with **argon2id** (sensible params). `verve account
  create` now sets a real password; add `verve account passwd`.
- **Sessions**: opaque signed session cookie (HttpOnly, Secure, SameSite).
  `sessions` table (or signed stateless token — pick one, document why).
- **Endpoints**: `POST /v1/auth/login`, `POST /v1/auth/logout`,
  `GET /v1/auth/me` (current Account + `Me` profile).
- **`authenticate` middleware**: cookie → Account, injected into request
  context. Structured so a future **forward-auth** mode (trust a reverse-proxy
  identity header) slots in without rework (ADR 0008).
- **Enforce scoping**: replace the dev flag/header from slice 04 — the API now
  derives the owner from the authenticated Account; unauthenticated data
  requests are rejected.
- **Rate limiting** on login to blunt brute-forcing.

## Out of scope

Forward-auth SSO implementation (only keep the seam). Account self-registration
UI (admin creates accounts via CLI for now) — revisit in v1.x.

## Acceptance

- Login with correct credentials sets a session cookie; wrong credentials are
  rejected and rate-limited.
- `GET /v1/series` without a valid session is rejected; with one, returns only
  the authenticated Account's data.
- Two Accounts never see each other's data.

## Refs

ADR 0007 (multi-user isolation), 0008 (local auth argon2id). `good_practices.md` §8.
