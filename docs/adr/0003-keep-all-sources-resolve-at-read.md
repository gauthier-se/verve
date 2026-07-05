# Keep all Sources, resolve overlap at read time

## Context

Multiple Sources report the same Metric over the same period: heart rate from
both Apple Watch and a Bluetooth device; steps from both Watch and iPhone (worn
together → double counting); nutrition from both Yazio and FatSecret. Apple
Health hides this by picking a priority source in its UI, but the export
contains everything, duplicated. Ingesting it all naively inflates step totals
and produces overlapping series.

## Decision

Store every Measurement with its Source (non-destructive). Resolve overlap at
read time using a per-Metric **Source priority**: a graph that needs one series
takes values from the highest-priority Source with data. Merging complementary
Sources (rather than choosing one) is deferred as a future refinement.

## Why

Non-destructive storage matches Verve's "data survives" philosophy (cf. the
Unmapped bin) and keeps the option to change resolution rules later. Choosing a
source at ingestion (destructive) is simpler to query but cannot be undone.
Read-time resolution can be materialized into a precomputed series later if
performance requires — that is an optimization, not a model change.

## Considered Options

- **Choose one Source at ingestion, discard the rest:** rejected — destructive,
  irreversible.
- **Store raw + materialize a resolved series eagerly:** deferred — a
  performance optimization to add if read-time resolution proves too slow.
