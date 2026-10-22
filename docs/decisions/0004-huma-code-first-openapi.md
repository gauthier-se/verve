# 0004. Huma for a code-first OpenAPI API

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

The same REST API serves the SPA, Health Auto Export, scripts and future integrations. We want end-to-end typing from Go to TypeScript, a public documentation that never drifts from the code, and consistent input validation.

## Decision

Use Huma v2 on top of `net/http` (via the `humago` adapter). Each operation is declared with typed Go input and output structs. Huma parses and validates requests from struct tags, returns RFC 9457 errors and generates an OpenAPI 3.1 spec. The spec is printed by `verve openapi` and used to generate the TypeScript client and TanStack Query hooks.

## Consequences

- The spec is always in sync with the code, and the public API documentation comes for free.
- Renaming a field in Go breaks the frontend type check, which is the desired behavior.
- Validation rules live next to the types.
- Endpoints that stream large bodies (ingestion) or serve files (exports, tus uploads, PMTiles) may be registered as plain `net/http` handlers and documented manually in the spec.

## Alternatives considered

- **Handwritten spec**: drifts from the implementation over time.
- **Spec-first with oapi-codegen**: solid, but editing YAML before every change is heavier day to day.
- **No spec**: loses typed client generation and public documentation.
