Status: needs-triage
Blocked by: 01

# 03: now, web: the Latest value read against its usual

## What

This is the sentence the feature exists for: "58 bpm, usual 46 to 52".

- The Now read (`internal/now`, ADR 0045) gives each Pin card the Usual of the
  day bucket its Latest value fell on, from the engine's `Series` at day grain, so
  the rule lives in one place.
- The in-progress rule from `01` applies: a `sum` Metric's card, whose latest day
  is today, shows no usual. Once the day is complete, it does.
- `PinCard` prints it under the figure in the muted text style, with no colour and
  no verdict. Absent when nil.

## Docs

- README: one line under the reading features.
- ROADMAP: the row, marked shipped.
- CONTEXT.md: the **Now** entry names the Usual beside the Goal.
