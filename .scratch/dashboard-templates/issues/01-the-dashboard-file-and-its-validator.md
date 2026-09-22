Status: done

# 01: dashtemplate, api: the Dashboard file format and the one validator

## What

- **New package `internal/dashtemplate`.** It owns the Dashboard file shape and
  the rules a Dashboard's arrangement must satisfy. No behaviour change for any
  existing route.

- **The shape**, decoded strictly (`DisallowUnknownFields`):

  ```json
  {
    "format": "verve.dashboard/1",
    "slug": "sleep",
    "name": "Sleep",
    "description": "How long, how deep, and how the body recovered overnight.",
    "range_preset": "30d",
    "baseline_rule": "none",
    "panels": [
      { "metrics": [{ "metric": "sleep", "chart_type": "stacked_bar" }], "width": 2 },
      { "metrics": [{ "metric": "resting_heart_rate", "chart_type": "line" }], "bucket": "week", "width": 1 }
    ]
  }
  ```

  `slug` and `description` belong to a template and are optional in the format,
  so a future export can omit them. `chart_type` is required in a file (a file
  says what it draws; the aggregation default is the HTTP convenience only).
  `bucket` is optional (auto). `width` defaults to 1. `custom` range and
  `custom` baseline are refused in a file: absolute dates are an Account's, not
  an arrangement's.

- **One validator, moved from `internal/api/dashboardhandlers.go`**:
  `validatePanelMetrics`, `validateChartType`, `defaultChartType`,
  `validatePanelBucket`, `validateWidth`, `maxPanelMetrics`, `maxPanelUnits`
  and `validChartTypes` move to `dashtemplate` as exported functions that
  report into a small error collector the API's `Validator` can absorb (field,
  message). The handlers call them; the error messages and field names returned
  over HTTP do not change.

- **`Validate(File) []FieldError`** checks the whole file: format version,
  name present and within the length cap, range and baseline tokens, one to
  twelve Panels, and every Panel through the moved rules. Errors name their
  place (`panels[2].metrics[1]`).

## Tests

- the existing `dashboardhandlers_test.go` passes unchanged, which is the proof
  the move changed no behaviour;
- a valid file decodes and validates clean;
- each refusal, one case each: unknown `format`, unknown field, unknown Metric,
  `band` on a `sum` Metric, `stacked_bar` on a non-by-state Metric,
  `diverging_bar` on an unsigned Metric, five Metrics, three units, bad bucket,
  width 4, `custom` range, zero Panels.

## Not

- No embedded templates yet, no route.

## Comments

- Faults are keyed `panels[i].<field>` (`panels[1].metrics`, `panels[0].chart_type`,
  `panels[0].bucket`), not `panels[i].metrics[j]`: the Panel rules are the HTTP
  ones unchanged, reported under the Panel's prefix, so the per-Metric index
  would have meant changing the keys the API returns.
- The API's `Validator` hands its own map to the moved rules
  (`dashtemplate.Invalid(v.Errors)`), so first-message-wins and every HTTP key
  and message are unchanged; `dashboardhandlers_test.go` passes untouched.
- `ValidateName`, `UnknownMetricMsg` and `MaxPanelMetrics` moved too (the series
  handler still caps a request at one Panel's worth through it).
- A file holds 1 to 12 Panels; a Dashboard built by hand keeps no cap.
