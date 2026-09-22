Status: done

# 04: api, web: a Goal on every Metric (ADR 0048)

- `internal/api/goalhandlers.go`: `validateGoalMetric` accepts any Catalog
  Metric. The test that refused `body_mass` becomes one that accepts it.
- An Attainment test on a `latest` Metric: days weighed are measured, a day
  with no reading is covered and not measured (no carry-forward).
- `web/src/lib/goals.ts`: `goalEligible` is removed with its tests; the Goal
  card shows on every Metric page.
- Verify: set a Goal on body mass, see the line on its Panel at day grain and the
  counts in the legend.
