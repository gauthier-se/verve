# Distinct data families, not one uniform sample type

## Context

Verve must store health data in a model that does not depend on Apple Health.
The obvious, KISS approach is a single `Sample { Type, Value, Unit, Start, End,
Source }` table, which cleanly covers ~90% of the volume (scalar time series
like heart rate and steps).

## Decision

The canonical model recognizes distinct **families** — Measurement, State,
Event, Session — each with its own shape and storage, rather than forcing all
data into one uniform sample type. Specialized data (ECG waveforms, nutrition)
will be modeled as refinements of these families, decided later.

## Why

A single scalar `Sample` breaks down for the non-scalar realities already
present in the sample data: ECG waveforms (thousands of points at 512 Hz),
sleep as a categorical state over an interval, workouts as rich aggregates with
GPS routes, and nutrition as a correlation of ~20 nutrients per meal. Collapsing
these into one abstraction would make both storage and visualization awkward
from day one. The cost of splitting them later (migrating a single fat table)
is high, so we pay the modeling cost up front.

## Consequences

- Storage and query paths differ per family; there is no single "samples" table.
- Connectors must classify incoming data into a family, not just dump rows.
