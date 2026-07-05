# Closed, extensible Metric Catalog with unit normalization

## Context

"Data independent of Apple Health" needs a concrete mechanism. Apple emits
`HKQuantityTypeIdentifierHeartRate` in `count/min`; other sources (Yazio,
Nike Run Club) use their own vocabularies and units. Something has to define the
canonical vocabulary.

## Decision

Verve owns a **Catalog**: a closed but extensible set of canonical Metrics, each
with a neutral slug (`heart_rate`), one canonical unit, and an aggregation rule.
Connectors must map incoming types to Catalog Metrics and normalize values to the
canonical unit at import time (keeping the original unit as metadata). Types a
Connector cannot map go to an **Unmapped bin** — kept and inspectable, never
discarded. The Catalog is seeded from Apple's ~90 types (well-designed) but
renamed to neutral slugs.

## Why

A closed Catalog guarantees clean data, reliable graphs, and well-defined units
and aggregations — impossible with free-form type strings, which degrade into a
dump (`heart_rate` vs `HR` vs `heartrate`). Normalizing units at import avoids
mixed-unit series and per-query conversion. The Unmapped bin removes the usual
downside of a closed vocabulary (silent data loss) by preserving anything the
Catalog does not yet cover.

## Considered Options

- **Open vocabulary (store types as-is):** rejected — no unit/aggregation
  guarantees, vocabulary becomes a dump.
- **Store original units, convert per query:** rejected — pushes complexity into
  every graph and risks mixed-unit series.
