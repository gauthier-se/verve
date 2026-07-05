# Idempotent re-import via content-key identity

## Context

An Apple Health export is a full snapshot every time (747 MB `export.xml`),
delivered as a `.zip` also containing `workout-routes/` and
`electrocardiograms/`. Its `Record` elements carry no stable ID. Re-importing a
later snapshot must not duplicate the entire history. The file is too large to
load into memory.

## Decision

Connectors parse the export by **streaming** (`xml.Decoder`, constant memory).
Deduplication uses a **Content key**: a hash of
`(metric, source, startDate, endDate, value, unit)`, with `creationDate`
excluded because it shifts between exports. Rows whose content key already exists
are skipped. Each Import is recorded with counts of added vs skipped rows. The
v1 entry point is a CLI (`verve import export.zip`); web upload comes later.

## Why

No stable source ID means identity must be derived from content. Excluding
`creationDate` keeps the key stable across exports. This makes re-import
idempotent with no Apple-side state. The rare collision (two legitimately
identical readings in the same second from the same source) is negligible for
health data. CLI import avoids fragile 747 MB browser uploads and suits homelab
use.
