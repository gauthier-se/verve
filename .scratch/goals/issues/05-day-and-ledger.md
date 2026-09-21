Status: done
Blocked by: 02

# 05: day, web: the Goal on the Day and in the Ledger, and the docs pass

## What

- **The Day** (`internal/day`, `day-page.tsx`): each `DayMetric` carries the Goal
  in force on that date, if any (direction and value), read through
  `GoalModel.InForce` over the one day. The page prints it beside the value:
  "7 912 · goal ≥ 7 500". No "met", no "missed", no icon: the value and the bound
  side by side say it. Today's row shows the bound too; it is the count that
  never judges today, not the page.
- **The Ledger detail table** (`ledger-detail-table.tsx`): at day grain, a Goal
  column showing the bound in force on each row's date, from
  `series.goal.segments`, so the count can be checked row by row. Absent above
  day grain.
- **Cross-metric is untouched.** A Goal changes neither a Co-variation nor its
  rank.
- **Docs**: a README line in the features list, and a ROADMAP row ("Goals: a
  declared daily bound per Metric, counted, never graded"). ADR 0044 and the
  CONTEXT.md entries already landed with the PRD.

## Tests

- a Day before any Goal carries none; a Day on the date a Goal changed carries
  the new one;
- a Day and the Panel agree on which Goal was in force on a given date;
- the Ledger column matches the segments at every row.

## Why here

The Day is where "was I there yesterday?" is asked, and the Ledger is where a
count is audited. Both only need the bound in force, which issues 01 and 02
already provide.

## Comments

Shipped. `Metric.Goal` and `goalOn` in `internal/day/day.go` with a test in
`day_test.go`, `DayGoal` in `types.ts` pinned by the contract test, the bound
beside the value in `day-page.tsx`, the Goal column in
`ledger-detail-table.tsx`, `boundText` and `segmentAt` in `lib/goals.ts`, a
README entry under "Fill the gaps and plan ahead" and a ROADMAP row.

What differs from the spec:

- **The Day reads the Account's Goals once** (`ListByAccount`) and picks the one
  in force per row, rather than one `InForce` query per Metric. `goalOn` spells
  the same half-open rule the segments use.
- **One way to write a bound.** `boundText` ("≥ 7 500", "≤ 2 300 mg",
  "≥ 7h 30m") is what the Panel's counts, the Day and the Ledger all print, so
  the three cannot describe one Goal differently.
- **The Ledger's Goal column is in the copied TSV too**, written the same way,
  since a column on screen and missing from the copy would be the one surprise
  in a table whose point is to be copied out.

Not driven in a browser. The Go suite, `tsc`, vitest and the production build
pass.
