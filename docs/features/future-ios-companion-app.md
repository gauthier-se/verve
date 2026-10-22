# iOS companion app

- **Status:** Planned
- **Milestone:** Future
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [API tokens](api-tokens.md), [ADR 0007](../decisions/0007-ingestion-strategy.md)

## Summary

A native, open source iOS app that reads HealthKit directly and pushes changes to Verve in near real time using the native ingest format, including HealthKit UUIDs and deletions. It removes the dependency on a third-party app and gives Verve exact, deduplicated raw data. This file captures the intent and the backend requirements so that the server is ready when work starts.

## User stories

- As a user, I want my data in Verve within minutes, without a paid third-party app.
- As a user, I want deletions made in the Health app to be reflected in Verve.
- As a user, I want to pair the app with my server by scanning a QR code.

## Scope

### In scope (when the work starts)

- SwiftUI app, HealthKit read permissions per type.
- Background delivery and incremental sync with anchors.
- Pairing with QR code.
- Sync status and history.

### Out of scope

- Writing to HealthKit.
- Displaying dashboards natively (the PWA covers it).

## Functional requirements (draft)

- **FR-1**: Pairing: the Verve web app shows a QR code containing the server URL and a newly created `ingest` token. The iOS app scans it and stores the token in the Keychain.
- **FR-2**: Incremental sync: `HKAnchoredObjectQuery` per type, anchors persisted on device, so that each sync sends only new samples and deleted object UUIDs.
- **FR-3**: Background: `HKObserverQuery` with `enableBackgroundDelivery` per type, triggering syncs when new data arrives, within iOS background limits.
- **FR-4**: Payloads use `verve.ingest.v1` with HealthKit UUIDs, full source and device information, workouts with routes (`HKWorkoutRouteQuery`) and events.
- **FR-5**: Anchors advance only after the server returns `202 Accepted`.
- **FR-6**: Settings: types to sync, Wi-Fi only option, initial backfill window.
- **FR-7**: Status screen: last sync per type, errors, server reachability.

## UX notes

- Minimal app: onboarding, permissions, status. Health data viewing stays in the web app.

## Data and API notes

- Backend requirements already covered: native ingest endpoint, idempotency on UUIDs, deletions, import status endpoint.
- Possibly needed: a pairing endpoint that creates a token for the QR code, and a lightweight endpoint returning server version and capabilities.

## Edge cases

- HealthKit data is unavailable while the device is locked: syncs happen when the device is unlocked or when background delivery wakes the app while unlocked.
- First sync of many years of data: chunked payloads with progress, or recommend the XML import for history first.

## Open questions

- Distribution: App Store or TestFlight (Apple Developer Program, 99 USD per year) versus sideloading (free provisioning expires after 7 days).
- Separate repository (`verve-ios`) or monorepo.

## Future ideas

- Home screen widgets with today's key values.
- Apple Shortcuts actions exposed by the app.
