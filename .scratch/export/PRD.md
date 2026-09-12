# PRD: Export

## Goal

Verve reads Apple Health and Google Takeout, keeps every source side by side,
refuses to discard what its Catalog cannot map, and says in its own README that
the canonical data outlives the source it came from. It has no way to get that
data back out.

The gap is not theoretical. Three requests hit it, and today each has the same
non-answer:

- **"Give me the numbers."** The Ledger already shows the aggregated series as a
  sortable table with the reading count behind every bucket (ADR 0021), which is
  exactly what a spreadsheet, an R session or a doctor's appointment wants, and
  the only way to get it out of the page is to select it with the mouse.
- **"Move me to the other machine."** Copying `VERVE_DATA_DIR` works and is the
  documented backup, and it is an answer only when the destination is Verve, the
  same version, the whole instance, every Account at once.
- **"Prove the data is mine."** A warehouse whose exit is `sqlite3` on a file
  format it never promised is a warehouse that asks to be trusted rather than one
  that can be checked.

The last is the one that matters. "Your data does not depend on the app it came
from" is the product's first sentence, and it is currently only true about
Apple. This milestone makes it true about Verve.

## The concept

**Two grains, and deliberately nothing between them.**

The first is **the numbers behind a curve**: a Series as CSV, one row per bucket,
the same figures the Panel drew and the Ledger tabulated, at the same bucket the
screen was showing. It is a download of a read.

The second is **an Archive**: every canonical row an Account holds, as stored,
with its artifacts, in one file that Verve can read back. It is a transfer of the
store.

Anything in between, a raw dump of `measurements` as one flat CSV, is the worst of
both: too big for a spreadsheet, lossy for a transfer (a flat CSV cannot carry a
Session's stats and its Routes as the one thing a workout is), and a third shape
to keep in sync with the two that have reasons to exist.

**An Archive is a transfer, not a read, and that is why it is not capped at the
day.** ADR 0012 caps the *read* API at day resolution because a chart with an
intra-day axis is a feature Verve has not built and a contract it does not want to
serve. The Archive serves no axis: it hands over the rows with the timestamps they
were stored with, which is the only thing a transfer can honestly do. A round trip
that rounded a heart-rate reading to its day would not be a transfer, it would be
a second, silent import filter.

**Re-reading an Archive is a Connector, not a restore.** The Connector contract
already describes exactly this: read a file the owner exported, write it through
the canonical Store, dedup by content key, report what was added and what was
skipped (ADR 0009, ADR 0035). A Verve Archive is a file the owner exported. So
`internal/connector/verve` is the third Connector, one line in the registry, and
the whole of the properties that follow come free: re-import is idempotent
(ADR 0006), an Import row records that it ran, and the Account's Exclusions are
honoured, so restoring your own history does not restore what you refused
(ADR 0033).

**The Archive carries no row ids, and it does carry the content key.** The two
halves have one reason. An id is a position in one database's tables and means
nothing in another, so nothing outside the exporting instance may depend on it. A
content key cannot be recomputed downstream: `data.ContentKey` hashes the raw
value string and the raw unit the *source* wrote, and Verve stores the normalized
value and `original_unit`, so a Connector recomputing a key from the stored fields
would mint a different identity for the same reading. Import an Archive beside a
fresh Apple export and every reading would be stored twice, once under each
derivation, which is the one failure ADR 0006 exists to prevent. So the key
travels, the Connector validates its shape and never its provenance, and the blast
radius of a forged one is bounded by `UNIQUE (account_id, content_key)`: it can
collide with a row in the Account doing the importing, and with nothing else. The
consequence is the milestone's defining test: export, import into an empty
Account, export again, and the two Archives' data files are byte-identical.

**Backup stays "copy the folder".** The Archive is portability and inspection: it
is one Account, it is readable without Verve, and it is version-independent by its
manifest. `VERVE_DATA_DIR` is the whole instance, every Account, the login
credentials and the WAL, and it stays the documented backup. Two mechanisms, two
sentences in the docs, no overlap to keep true.

**Archive**:
The file Verve writes holding one Account's canonical data as stored, plus its
artifacts and a manifest, which the Verve Connector reads back.
_Avoid_: Export (taken, and precisely: an Export is what a *source* produces and a
Connector reads, ADR 0035, which is what an Archive becomes once it is read back),
Backup (copying the data directory is the backup), Dump (implies the storage
format rather than the canonical model), Snapshot (implies restoring an instance
to a point in time, which this cannot do).

## What this milestone does

- **An Archive is a zip**, laid out as the write seam is, not as the tables are:
  ```
  manifest.json
  measurements.ndjson
  states.ndjson
  sessions.ndjson        each Session with its stats and its Routes nested
  unmapped.ndjson
  imports.ndjson         provenance, never replayed
  artifacts/<sha256>.gpx
  ```
  NDJSON because it streams on both sides and a person can `grep` it; nested
  Sessions because `InsertWorkout` writes a Session, its stats and its Routes as
  one unit and the file has no reason to disagree with the store's own seam
  (ADR 0038).
- **The Unmapped bin is in it.** "Nothing is discarded" is the reason the bin
  exists (ADR 0002); an Archive that dropped it would lose exactly what Verve
  promised to keep, and would lose it at the one moment the owner was told they
  were taking everything.
- **The manifest carries what is not a row**: `schema` version, the Verve version
  that wrote it, `exported_at`, per-family counts, the Account's profile fields
  (`date_of_birth`, `biological_sex`, `blood_type`, read by the basal estimates,
  ADR 0023), and a `warnings` array naming any Route whose artifact was missing
  from disk. No email, no password hash: an Archive identifies data, not a login.
- **The data files are deterministic**, ordered by `(start_at, content_key)` and
  written with fixed field order, so two Archives of the same data differ only in
  the manifest's `exported_at`. Diffing two months of backups is then a real
  operation rather than a guess.
- **Reading is batched, like writing.** `internal/data` gains keyset-paged
  iterators per family, and the writer streams straight into the zip, so peak
  memory is one batch and an Account with five million Measurements exports on a
  Raspberry Pi.
- **`verve export --account=EMAIL FILE`**, beside `import` in the same CLI, with a
  summary naming the counts per family, so the round trip is testable and
  scriptable without a browser.
- **`GET /v1/export/archive`**, authenticated and Account-scoped (ADR 0007),
  streamed as `application/zip` with a `Content-Disposition` filename dated to the
  day. It is a plain streamed read and not an import-style job: nothing is written,
  so there is no state to poll, and cancelling is closing the connection.
- **`GET /v1/series.csv`**, taking exactly the parameters `/v1/series` takes and
  calling the same read module (ADR 0037), so the file and the curve cannot
  disagree. RFC 4180, UTF-8, no BOM, header row
  `bucket_start,bucket_end,metric,unit,aggregation,value,count`, one row per
  bucket per Metric, and **a gap is an empty cell, never a zero** (ADR 0014,
  ADR 0032). Its own contract test, pinned like the JSON one.
- **`internal/connector/verve`**, the third Connector: `Accepts` opens the zip and
  reads `manifest.json` for its schema marker, which is disjoint from Apple's
  `export.xml` and Google's Takeout tree by construction; `Import` writes through
  the same `connector.Store`, recomputing every content key; the report is the
  same `Report` every other Connector fills in. One line in `registry.connectors`.
  Both import paths, web and CLI, get it without knowing it exists.
- **The Verve Connector is the one Connector that may carry the `Manual` source.**
  A Manual Measurement is an Account's own typed figure (ADR 0022) and the
  Exclusions PRD had to say it was gone for good, because no export held it. An
  Archive holds it, and re-importing it restores it *as* Manual: still deletable,
  still overlaying the imported day. It is restoring what the owner wrote, not a
  Connector inventing a hand-typed row, and the Connector refuses `Manual` on any
  Record whose Archive schema it did not just read.
- **An unreadable Archive fails before it writes.** A truncated zip, a missing
  manifest, an unknown `schema` version, or an NDJSON line the schema does not
  describe stops the import with a message naming the file and the line, rather
  than importing the prefix that happened to parse.
- **The web gets both downloads.** An **Export** card on `/import`, under the
  Exclusions list and the drop zone, with what the Archive contains in one
  sentence and a plain warning that it is the owner's whole history in clear; and
  a CSV button on the Data page's per-Metric table and on the Metric page, which
  downloads the window and bucket currently on screen. The nav label becomes
  "Import & export": nobody looks for their data's exit under "Import".

## What this milestone does NOT do

- **No Dashboards, Panels, Pins, Phases or Annotations in the Archive.** They are
  arrangement and commentary, not canonical data; the Connector `Store` cannot
  write them and widening it so an Archive can round-trip a grid layout is how a
  transfer format turns into a second serialization of the whole app. Sharing a
  Dashboard as JSON is its own feature, the same shape as a contributed palette,
  and it is listed in the Docs below as the successor rather than smuggled in
  here.
- **No profile applied on import.** The manifest carries it so nothing is lost; a
  Connector writes families, not the Account it is writing for. The report says
  the Archive carried a profile and points at the Profile page.
- **No encryption, and no password on the zip.** Zip's own crypto is not worth
  offering, and an owner who needs an encrypted Archive has `age` or `gpg` and a
  pipe. The interface says the file is in clear instead of implying otherwise.
- **No flat CSV of raw Measurements.** See the concept: two grains, nothing
  between.
- **No CSV for Sessions, the Ledger overview or the co-variation table.** Each is a
  different shape with a different natural key, and each would want its own
  columns; the Series CSV is the one that answers "the numbers behind the curve",
  which is the request. The Archive already carries every workout.
- **No Baseline columns in the CSV.** The comparison window is a second Series and
  a second download, not two value columns whose dates do not line up (ADR 0015's
  ordinal alignment exists on screen, and it is the screen's business).
- **No progress bar on the download.** The Archive streams, so its length is not
  known when the headers go out, and a browser download is the browser's job. The
  import's progress exists because an import is work Verve does; an export is
  bytes Verve hands over.
- **No `exports` table and no History event.** The History explains the shape of
  the data (ADR 0032) and an export does not change it. A record of who took a
  copy is an audit feature for a product with an administrator, which this is not.
- **No scheduled or automatic export.** A `verve export` in a cron line is the
  feature, and it exists the day the command does.
- **No sub-day read endpoint.** The Archive is a file, not an axis; `/v1/series`
  and everything else stay capped at the day (ADR 0012).

## Ordering

`01` defines the format and proves it from the CLI, so `02` has something real to
read back and the round-trip test can exist before any HTTP is involved. `03`
exposes both downloads over the API, `04` puts them on screen. `02` and `03` both
depend on `01` and on nothing else, so they can run in parallel.

## Docs

- **A new ADR** (`0039`): an export is two grains, a Series CSV that is a read and
  an Archive that is a transfer, and reading an Archive back is a Connector rather
  than a restore path. It records why the Archive is exempt from ADR 0012's day
  cap (a transfer serves no axis), why it carries content keys but no ids, and what
  was turned down: a flat raw CSV, `format=csv` on every existing endpoint, the
  SQLite file as the transfer format, Dashboards and Pins inside the Archive, an
  encrypted zip, an `exports` audit table, and recomputing content keys on the way
  back in. It is the sequel to ADR 0035, which
  said a Connector reads a file the owner exports, and now Verve is one of the
  things the owner exports from.
- **A CONTEXT.md entry** for **Archive** with its `_Avoid_` list, in the
  cross-cutting section beside **Import** and **Content key**, since it belongs to
  the transfer path and to no data family in particular.
- **ADR 0022's "gone for good"** gets a one-line amendment pointing here: a Manual
  Measurement is restorable from an Archive.
- **README.md**: a bullet under "Own your data" for the Archive and the CSV, and
  the line in "What Verve does not do" about the hosted version stays exactly as
  it is, because this is the feature that makes it defensible.
- **docs/deployment.md**: the backup section gains the distinction in two
  sentences, copy the directory to back up the instance, take an Archive to move
  one Account or to read the data elsewhere.
- **ROADMAP.md**: a shipped row, and a new "Later" entry for sharing a Dashboard as
  JSON, which this milestone deliberately left out.

## Issues

1. `01-archive-format-writer-and-cli`, data and cli: the manifest and NDJSON
   schemas, `internal/archive`'s streaming writer, the keyset-paged family
   iterators in `internal/data`, `verve export`, the determinism test, the ADR and
   the CONTEXT.md entry.
2. `02-verve-connector-reads-the-archive`, connector: `internal/connector/verve`,
   detection by manifest, the reader and its refusals, the carried content key and
   its validation, the `Manual` source rule, an unknown slug routed to the Unmapped
   bin, the registry line, and the round-trip byte-identity test through an empty
   Account.
3. `03-export-endpoints`, api: `GET /v1/export/archive` streamed and
   Account-scoped, `GET /v1/series.csv` over the same read module, both pinned by
   contract tests.
4. `04-web-export-card-and-csv-buttons`, web: the Export card on `/import`, the
   nav label, the CSV buttons on the Data and Metric pages carrying the window and
   bucket on screen, and the docs pass.
