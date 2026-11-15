# 0015. A handwritten tus server for XML uploads

- **Status:** Accepted
- **Date:** 2026-11-15

## Context

An Apple Health export weighs up to several gigabytes, and is often uploaded from an iPhone over a mobile link, where a single request of that size fails long before it ends. [XML import](../features/ingestion-xml-import.md) FR-2 therefore uses tus, a resumable upload protocol that `tus-js-client` speaks in the browser. The server side has to keep an incomplete upload, its offset and its expiry, check the session and the CSRF protection of every state-changing request, and hand a completed upload to the import pipeline, whose imports of every origin already read an inbox file.

## Decision

- `/api/v1/uploads/` is a plain `net/http` handler written in Verve, as [ADR 0004](0004-huma-code-first-openapi.md) allows for endpoints that stream large bodies, and documented manually in the OpenAPI spec.
- It serves tus 1.0.0 with the `creation`, `termination` and `expiration` extensions only: the three that `tus-js-client` needs to create, resume and expire an upload. `Upload-Defer-Length`, concatenation and checksums are not offered.
- Incomplete uploads live in the table `uploads`, next to the imports, and their files in `/data/uploads`. The handler runs behind the session and CSRF middleware of every request.
- A completed upload is renamed into the inbox, and its import and job are created in one transaction, so that from then on it has the lifecycle of every inbox file: retry after a failure, cleanup, cancellation.

## Consequences

- Verve owns the protocol code: offsets, expiry, the `409` of a wrong offset, the `507` of a full disk. The contract of the three extensions is small enough to cover with handler tests.
- No dependency is added on the server.
- Only incomplete uploads differ from the other imports. Their cleanup is one recurring job, `cleanup.uploads`, and account deletion removes them with the other data of the user.
- An extension that a later client needs, such as checksums, is code to write, not an option to turn on.

## Alternatives considered

- **`tusd` as a library**: it brings its own storage of uploads, its own locking and its hooks, of which Verve would use none, and its storage would sit outside the database that holds the imports. Plugging the session, the CSRF check and the handover to the inbox into it would take hooks around every request.
- **A plain multipart upload**: no dependency either, but an upload cut by the network starts again from zero, which an export of several gigabytes over a mobile link cannot afford.
