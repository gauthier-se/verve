# Public API

- **Status:** Planned
- **Milestone:** M4
- **Related:** [API tokens](api-tokens.md), [Storage and aggregates](data-storage-and-aggregates.md), [ADR 0004](../decisions/0004-huma-code-first-openapi.md)

## Summary

The REST API used by the SPA is also the public API. It is described by an OpenAPI 3.1 spec generated from the Go code, browsable in an interactive documentation page, and usable with `read` tokens by scripts, Grafana (through a JSON data source plugin) or Home Assistant REST sensors.

## User stories

- As a homelabber, I want to display today's steps and last night's sleep in Home Assistant.
- As a data-minded user, I want to query my aggregates from a script.
- As a developer, I want accurate, always up-to-date API documentation.

## Scope

### In scope

- Versioned API under `/api/v1`, OpenAPI spec, interactive docs.
- Stability policy.
- Convenience endpoints for integrations.
- Examples for Home Assistant and Grafana in the documentation.

### Out of scope

- Write endpoints for tokens beyond ingestion.
- GraphQL.

## Functional requirements

- **FR-1**: The OpenAPI 3.1 spec is served at `/api/openapi.json` and `/api/openapi.yaml`. Interactive documentation at `/api/docs`, with its assets bundled in the binary, never loaded from a third-party CDN. Both require no authentication but expose no data. The documentation page comes with M4: until then `/api/docs` returns the JSON 404 problem of unknown API paths.
- **FR-2**: All endpoints that read the owner's health data accept either a session or a `read` token.
- **FR-3**: Stability: endpoints are marked stable or experimental in the spec. Stable `v1` endpoints only receive backward-compatible changes. Breaking changes go to `v2`, with `v1` kept for at least one minor release cycle after deprecation.
- **FR-4**: Convenience endpoint `GET /api/v1/summary/today` returns a flat JSON object with the main daily values (steps, active energy, exercise minutes, resting heart rate, HRV, last night's total sleep, trend weight, calories consumed and target), designed for Home Assistant `rest` sensors.
- **FR-5**: List endpoints use cursor pagination with a consistent shape.
- **FR-6**: Errors use RFC 9457 problem details with stable codes.
- **FR-7**: Rate limits per token, returned with standard headers.
- **FR-8**: Documentation includes copy-paste examples: Home Assistant `rest` sensor configuration, Grafana Infinity data source query, curl examples.

## UX notes

- Link to the API documentation from the tokens page.

## Data and API notes

- Endpoint families: metrics catalog and series, samples, sleep nights, workouts and streams, records, energy estimates, plans (read), summary.
- The spec is committed in the repository and diffed in CI to catch unintended breaking changes.

## Edge cases

- Large ranges requested with `bucket=hour`: rejected with a clear error suggesting a coarser bucket, to protect small servers.

## Acceptance criteria

- [ ] Every endpoint used by the SPA appears in the spec.
- [ ] The Home Assistant example works against a test instance.
- [ ] CI fails on a breaking change to a stable endpoint.

## Future ideas

- Official Home Assistant integration with MQTT discovery.
- Webhooks on new data.
