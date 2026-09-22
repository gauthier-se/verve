Status: done
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

## Comments

`query.Latest` is unchanged: the Ledger reads it too and has no use for a second
read. `now.Engine.usualOn` asks the engine for the Latest day's own day bucket
with `Usual: true`, so the card and the Panel cannot disagree. Today is skipped
before the read, not cleared after it. The card prints `usualLine` under the date,
with the bounds formatted as the Goal's are. Tests in `internal/now/usual_test.go`.
