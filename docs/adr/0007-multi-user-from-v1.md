# Multi-user with strict isolation from v1

## Context

Verve runs in a homelab. Two forces push beyond single-user: a household (family
members wanting their own data on one instance) and the OSS community (people
hosting for several users). Health data is intimate, so any sharing of an
instance demands strict per-person isolation. Retrofitting isolation onto health
data later is a painful and privacy-risky migration.

## Decision

Build Verve **multi-user from v1**, not just multi-user-ready. Every row belongs
to exactly one **Account**; Accounts never see each other's data. Authentication
and account management ship in v1.

## Why

Chosen over the cheaper "model an Owner but seed a single account, no auth"
compromise because the user wants real multi-user immediately. Baking isolation
in from the start avoids a schema-wide migration and the data-leak risk of
adding it later to personal health data.

## Consequences

- Every table carries an owning Account; every query is Account-scoped.
- v1 must include auth and account management (see the auth-mechanism ADR).
- Ingestion (CLI import), Dashboards, and Annotations are all Account-scoped.
