# 0007. Ingestion strategy: XML import, Health Auto Export and a native format

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Apple provides no server-side API for Health data. HealthKit is only reachable from an app running on the iPhone. The options are the manual export (`export.zip`, often 1 to 5 GB), third-party apps that push data (Health Auto Export), Apple Shortcuts, or a custom iOS app.

## Decision

- **History**: import the Apple Health export (`export.zip` or `export.xml`), uploaded from the web app or imported with the CLI.
- **Continuous sync**: accept scheduled JSON pushes from Health Auto Export (export format version 2).
- **Native format**: the pipeline works on Verve's own normalized records, and Verve exposes a native ingest endpoint with its own versioned format. Health Auto Export and the XML export are adapters that convert into these records.
- **Later**: a native iOS companion app will use the native format, including HealthKit UUIDs and deletions.
- **Not planned now**: Apple Shortcuts.

## Consequences

- Users get their full history and continuous sync without waiting for a custom iOS app.
- Health Auto Export is a paid app and its format is controlled by a third party. Isolating it in an adapter limits the impact of format changes.
- The companion app will not require backend changes beyond what the native format already supports.

## Alternatives considered

- **Companion app first**: real-time and fully controlled, but a large effort and requires an Apple Developer account to avoid reinstalling every 7 days.
- **Shortcuts only**: free but limited data types and fragile automations.
- **XML only**: no continuous sync.
