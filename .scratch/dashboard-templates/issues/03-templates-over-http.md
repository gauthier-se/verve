Status: ready-for-agent
Blocked by: 02

# 03: dashtemplate, query, api: listing and instantiating templates

## What

- **`GET /v1/dashboard-templates`** (auth required): every template but the
  seeded one, in roster order.

  ```json
  { "templates": [
    { "slug": "sleep", "name": "Sleep", "description": "…",
      "metrics": ["sleep", "heart_rate_variability_sdnn", "…"],
      "with_data": 4, "panels": 5 }
  ] }
  ```

  `metrics` is the distinct Metrics across the Panels, in first-seen order.
  `with_data` counts the ones in `query.Engine.MetricsWithData` for the Account.
  Check how that answers a **derived** Metric: if it lists only stored
  Metrics, a derived one counts as having data when every operand of its Formula
  does, decided in one helper next to `MetricsWithData`, not in the handler.

  The Overview is left out: every Account already has it, or deleted it on
  purpose. It stays reachable by slug for `POST`.

- **`POST /v1/dashboards` accepts `template`**: `{"template": "sleep"}` or
  `{"template": "sleep", "name": "My sleep"}`. With a template, `name` is
  optional and defaults to the template's. An unknown slug is a 422 on
  `template`. Without `template`, the route behaves exactly as today. The
  response is the created Dashboard with its Panels, as `GET /v1/dashboards/{id}`
  returns it, so the client lands on it without a second read.

- **`DashboardTemplate` in `web/src/lib/types.ts`**, pinned in
  `contract_test.go`.

## Tests

- the listing omits the Overview and carries `with_data` against a seeded
  fixture Account (one with sleep and no HRV, say);
- instantiating `sleep` writes a Dashboard with the template's Panels, in order,
  with their chart types, buckets and widths;
- instantiating twice makes two Dashboards;
- a name override is used and validated like any name;
- an unknown template is a 422 and writes nothing;
- another Account's list is unaffected (ADR 0007).
