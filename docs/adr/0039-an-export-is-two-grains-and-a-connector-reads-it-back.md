# An export is two grains, and a Connector reads it back

## Context

Verve reads Apple Health and Google Takeout, keeps every Source side by side,
refuses to discard what its Catalog cannot map, and opens its README with the
claim that the canonical data outlives the app it came from. It had no way to get
that data back out.

Three requests hit the same gap. "Give me the numbers": the Ledger shows the
aggregated series as a table with the reading count behind every bucket (ADR
0021), and the only way to extract it was the mouse. "Move me to the other
machine": copying `VERVE_DATA_DIR` works, and is an answer only when the
destination is Verve, the same version, the whole instance, every Account. "Prove
the data is mine": a warehouse whose exit is `sqlite3` over a schema it never
promised asks to be trusted rather than offering to be checked.

The last one is the one that matters, and it made the claim true about Apple and
not about Verve.

## Decision

**Two grains, and deliberately nothing between them.**

The first is a **Series as CSV**: one row per bucket, the figures the Panel drew
and the Ledger tabulated, at the bucket on screen. It is a download of a read,
served by the same read module the chart uses (ADR 0037), so the file and the
curve cannot disagree.

The second is an **Archive**: every canonical row an Account holds, as stored,
with its artifacts and a manifest, in one zip. It is a transfer of the store.

A flat raw CSV of `measurements` would be the worst of both: too big for a
spreadsheet, and lossy for a transfer, because a flat file cannot carry a Session
with its stats and its Routes as the one thing a workout is.

**An Archive is a transfer, not a read, so ADR 0012's day cap does not bind it.**
That cap exists because a chart with an intra-day axis is a feature Verve has not
built and a contract it does not want to serve. An Archive serves no axis: it
hands over rows with the timestamps they were stored with. Rounding a heart-rate
reading to its day on the way out would not be a transfer, it would be a second,
silent import filter.

**Reading an Archive back is a Connector.** `internal/connector/verve` reads a
file the owner exported, writes through the canonical Store, dedups by content
key, records an Import run and reports what it added: the contract already
describes exactly this (ADR 0009, ADR 0035). Every property a bespoke restore
path would have had to reinvent comes with it, including that an Account's
Exclusions are honoured, so restoring your own history does not restore what you
refused (ADR 0033).

**The Archive carries no row ids, and it does carry the content key.** An id is a
position in one database's tables and means nothing in another. A content key
cannot be recomputed downstream: `data.ContentKey` hashes the raw value string
and raw unit the *source* wrote, while the row holds the normalized value and
`original_unit`. A Connector minting its own key would give the same reading a
second identity, and importing an Archive beside a fresh export of the same
source would store everything twice, which is the one failure ADR 0006 exists to
prevent. So the key travels, the reader validates its shape and never its
provenance, and a forged one is bounded by `UNIQUE (account_id, content_key)`: it
can collide with a row in the importing Account, and with nothing else.

**Backup stays "copy the folder".** The Archive is portability and inspection:
one Account, readable without Verve, version-independent by its manifest.
`VERVE_DATA_DIR` is the whole instance, every Account, the credentials and the
WAL, and it remains the documented backup.

## Why

- **The round trip is the test.** Export, import into an empty Account, export
  again, and the two Archives' data files are byte-identical. One assertion pins
  the format, the ordering, the key carriage and every family at once, and it
  fails the day any change makes the Archive lossy. It is also why the data
  entries are written in a total, index-aligned order with zeroed zip timestamps:
  determinism is not a nicety here, it is what makes the property assertable.
- **The Connector contract was already the right shape.** A `--restore` flag
  would have re-implemented idempotence, the Import record, the Report and the
  Exclusions, slightly differently, and then owed an explanation for why
  restoring an Archive does not appear in the History while importing an export
  does.
- **A manifest, not a schema guess.** The version is checked on the way in and a
  newer one is refused, because a reader that keeps going reads the fields it
  recognizes and drops, in silence, whatever the newer version added.
- **The Unmapped bin travels.** It exists so nothing incoming is lost to a
  Catalog gap (ADR 0002). An Archive that dropped it would lose exactly what
  Verve promised to keep, at the moment the owner was told they were taking
  everything.

## Considered Options

- **A flat CSV of raw Measurements as the transfer format.** Rejected: see the
  two grains. It cannot carry a workout, and nothing can read it back without
  inventing an identity for every row.
- **`format=csv` on every existing endpoint.** Rejected: it would put a
  serialization branch in handlers that currently do one thing, and it invites the
  question of what a CSV of the co-variation matrix or the History looks like. One
  endpoint, one contract, one golden test.
- **Shipping the SQLite file as the export.** Rejected: it is the backup, it is
  every Account at once, and it promises a schema Verve changes with every
  migration. The point of a canonical model is that the transfer does not depend
  on the storage.
- **Recomputing content keys on the way back in.** Rejected: it cannot be done
  from the stored fields, and doing it from a different preimage would double
  every reading that also arrives from its original source.
- **Dashboards, Panels, Pins, Phases and Annotations inside the Archive.**
  Rejected for now: they are arrangement and commentary, not canonical data, and
  the Connector `Store` cannot write them. Widening it so a transfer format can
  round-trip a grid layout is how a transfer becomes a second serialization of the
  whole application. Sharing a Dashboard as JSON is its own feature.
- **Applying the manifest's profile on import.** Rejected: a Connector writes
  families, not the Account it writes for. The profile travels so nothing is lost,
  and the report points at the Profile page.
- **An encrypted or password-protected zip.** Rejected: zip's own crypto is not
  worth offering, and `verve export --account=me - | age -r …` is one pipe. The
  interface says the file is in clear rather than implying otherwise.
- **An `exports` table and a History event.** Rejected: the History explains the
  shape of the data (ADR 0032) and an export does not change it. A record of who
  took a copy is an audit feature for a product with an administrator.

## Consequences

- `internal/archive` owns the format, the manifest and the writer, and reads
  through a `Source` interface, so the format is testable without SQLite and
  `internal/data` never learns about zip.
- `internal/data` gains keyset-paged reads per family, ordered by each table's own
  index prefix and tie-broken by `content_key`. The order is total, so paging can
  neither repeat nor skip a row, and reproducible, so two exports match.
- The export runs inside one read transaction through the seam of ADR 0038, so an
  import running beside it cannot land half of itself in the middle of the file.
  It holds the single connection for the duration, which is the price of an
  Archive that is one consistent moment.
- A Route whose artifact is missing from disk still travels as a row, with a
  warning in the export summary. The track is gone either way; dropping the Route
  too would lose the fact that the workout had one.
- ADR 0022 said a Manual Measurement is gone for good once purged, because no
  export held it. An Archive holds it, and the Verve Connector is the one
  Connector that may write the `Manual` source, because it is restoring what the
  owner typed rather than inventing a hand-typed row.
- The Archive endpoint streams without a `Content-Length`, so a browser download
  shows no progress. An error after the first byte cannot change the status code,
  so the writer is left unfinalized: an unzip tool refuses the file loudly, which
  is the only honest signal left at that point.
