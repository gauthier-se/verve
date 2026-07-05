# Broad ingestion, focused v1 UI

## Context

Apple exports ~90 record types. Mapping a type to a canonical Metric is cheap
declarative data (see the Connector ADR). Building a graphable Panel for each is
not. "What we capture" and "what we show" need not have the same scope.

## Decision

Seed a **broad Catalog** in v1 — nearly all ~90 Apple types mapped to canonical
Metrics — so ingestion captures almost everything and the Unmapped bin stays
near-empty. Ship a **focused v1 UI** covering ~20-25 high-value Metrics
(activity, heart, body, sleep, nutrition macros, respiratory, SpO2). Workout
**Sessions** (with GPX routes) are ingested in v1, but the rich workout UI (list,
detail, map) lands in v1.x. Apple's `Me` becomes static Account profile fields.

## Why

Mapping is nearly free and non-destructive, so capturing broadly means any
Metric can be graphed later *retroactively* over data already stored — no
re-import, no migration. Building UI is expensive, so it stays focused on what
delivers daily value first. This decouples data coverage from UI surface and
explains why the Catalog intentionally contains Metrics with no Panel yet.
