Status: ready-for-agent
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
