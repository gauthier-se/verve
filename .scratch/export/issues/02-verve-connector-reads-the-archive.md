Status: done
Blocked by: 01

# 02: connector: the third Connector reads a Verve Archive

## What

- **`internal/connector/verve/`**, a package with the shape of its two
  neighbours and one thing neither has: **no mapping table**. Every other
  Connector's bulk is the declarative map from a source type to a Catalog slug
  (ADR 0009); this one's source is already the canonical model, so the package is
  a reader, a validator and a batcher. Say so at the top of `connector.go`, or the
  next reader will look for the missing file.
- **`Name() == "verve"`, `Label() == "Verve Archive"`.** The name is what an
  Import row records, and it is what the History will show on the day an Account
  restored itself.
- **`Accepts(path)`** opens the zip, reads `manifest.json` alone, and returns true
  on `verve_archive` being present. Cheap, by content, and disjoint from Apple's
  `export.xml` and Google's Takeout tree by construction, so `registry.For` stays
  order-independent (`registry.go:22`).
- **`Import`** reads the entries in a fixed order: manifest, measurements, states,
  sessions, unmapped. `imports.ndjson` is **read and ignored**, deliberately: it is
  provenance for a human reading the file, and replaying it would fabricate import
  runs that never happened on this instance. The run records its own single Import
  row like every other Connector does.
- **The schema check is the first refusal.** `Schema > archive.Schema` stops with
  a message naming both versions and saying to upgrade Verve, because a reader
  that keeps going reads the fields it recognizes and silently drops the family
  the newer version added. `Schema < archive.Schema` is read, and today there is
  no such version; the branch exists so the message is written once rather than in
  a panic later.
- **Every row is validated before it is written**, and a bad one stops the import
  naming the entry and the line number:
  - the content key is 64 lowercase hex characters (its shape, never its
    provenance: see below);
  - `start_at` and `end_at` parse as RFC 3339 and `end_at >= start_at`;
  - a Measurement's `metric` is a Catalog slug **or** goes to the bin (next
    bullet), and its `value` is a finite float;
  - a Session's nested stats name a Catalog slug and one of the four aggregates,
    matching the `session_stats` primary key.
  Nothing is written before the manifest and the first page validate, so a
  truncated Archive fails rather than importing the prefix that parsed.
- **An unknown Metric slug goes to the Unmapped bin**, with `source_type` set to
  the slug. An Archive written by a newer Verve can carry a Metric this binary's
  Catalog does not have, and the bin is exactly the place for a record whose type
  the Catalog cannot map (ADR 0002). It is also a downgrade path: upgrade later
  and re-import, and the row lands as a Measurement, with the bin copy left
  behind, which is what already happens whenever the Catalog grows.
- **The Account's Exclusions apply, including to `Manual` rows.** The per-record
  check is `opts.Exclusions.Excludes(metric, startAt)` (`exclusion.go:159`), the
  same call `applehealth` makes, counted into `Report.Excluded` and the per-Metric
  `Tally`. The `Manual` part is the one judgement call: ADR 0033 says an Exclusion
  governs Connectors and never the Account, and the Exclusion's own purge already
  deletes `Manual` rows. Re-admitting one from an Archive would undo a purge the
  owner asked for, so the standing refusal wins over the older typed value. Test
  it, and comment it where the check is.
- **This is the one Connector that may write `catalog.SourceManual`.** It is
  restoring a figure the owner typed, not inventing a hand-typed row, and the
  restored row is still `Manual`, so it still overlays its day and is still
  deletable through `measurement.go:99`, whose guard keys on the source. That
  single sentence is what makes an Archive a real backup of the one family
  nothing else can restore (ADR 0022, amended by the PRD's Docs section).
- **Progress is exact here.** `zip.Reader` knows every entry's
  `UncompressedSize64`, so `opts.Progress(decoded, total)` reports real bytes
  against a real total rather than the estimate the other two Connectors give.
  The web import gets a progress bar that is right for once, at no cost.
- **Batching is `connector.BatchSize`**, flushed per family through the same
  `Store` methods, with `InsertWorkout` for a Session and its nested stats and
  Routes. Routes: copy `artifacts/<name>` out of the zip into
  `opts.ArtifactsDir` before the row is written, skipping a file already there,
  since the name is its content hash. A Route whose artifact is absent from the
  zip is written as a row anyway, matching what the writer does in issue 01.
- **One line in `registry.connectors`** (`registry.go:24`), after `googlehealth`.
  Both import paths, the web upload and `verve import`, then read an Archive
  without knowing it exists, which is the whole point of the contract.
- **Tests** in `internal/connector/verve/import_test.go`:
  - **the round trip, which is the milestone**: seed an Account, export with
    `archive.Write`, import into an empty Account, export again, and assert the
    two Archives' data entries are byte-identical. It pins the format, the
    ordering, the key carriage and every family at once, and it is the test that
    fails if any later change makes the Archive lossy;
  - **idempotence**: importing the same Archive twice adds nothing the second
    time and reports it all as skipped (ADR 0006);
  - **beside a source export**: import an Apple fixture, export, import the
    Archive back into the same Account, and assert nothing was added. This is the
    test that would have caught a recomputed content key;
  - **Exclusions**: an excluded Metric's rows are refused and counted, including a
    `Manual` one;
  - **`Manual` survives**: a Manual Measurement round-trips as `Manual` and the
    restored row is deletable through the API's guard;
  - **an unknown slug** lands in the bin with the slug as its `source_type`;
  - **refusals**: a newer `schema`, a missing manifest, a truncated zip, a
    non-hex content key and an inverted interval each fail, each naming the file,
    and each leaving the Account with nothing written;
  - **`Accepts` is disjoint**: the Apple and Google fixtures already in the repo
    are rejected, and an Archive is rejected by their `Accepts`.

## Why here

Reading an Archive is a Connector and not a `--restore` flag because every
property a restore would have to invent already exists in the contract:
idempotence by content key, a recorded Import run, a Report the CLI and the web
both render, and the Exclusions. Writing it anywhere else means writing those
five again, slightly differently, and then explaining why restoring an Archive
does not appear in the History while importing an export does.

The validation stopping the import rather than skipping the bad line is the
opposite of what the Unmapped bin does, and the difference matters: the bin
exists for data a *source* produced that Verve cannot map, which is expected and
common. A malformed line in a Verve Archive means the file is damaged or
hand-edited, and the useful answer is the line number, not a partial import whose
missing rows nobody will ever notice.

The `Manual` rule cuts both ways in one paragraph, and both cuts are deliberate:
an Archive may restore a Manual row (nothing else can), and an Exclusion may
refuse one (the owner said so more recently than the day they typed it).

## Comments

Shipped. `internal/connector/verve/connector.go` (name, label, detection by
manifest marker), `internal/connector/verve/import.go` (the reader, the
validation, the artifact copy, the counts check), and the registry line. Tests:
`internal/connector/verve/import_test.go`, including the round trip across two
separate instances. Whole suite green.

Four things came out of the implementation that the spec did not anticipate:

- **The Connector is almost entirely `connector.Sink`.** The spec described
  batching, the per-record Exclusion check, the tallies and the Import record as
  work this package would do; all four already live in the Sink, which every
  Connector shares. So the `Manual`-rows-are-excluded-too decision needed no code
  at all: the Sink applies `Excludes` to every Measurement alike, and the
  judgement turned out to be the one the contract already made. What is left here
  is reading the file and validating it.
- **The manifest's counts are checked against what was read.** Not in the spec,
  and it is the cheap half of the integrity story: a zip's central directory
  proves the entries are whole, not that they hold every row the export meant to
  write, and an Archive that is short by a thousand rows is otherwise
  indistinguishable from one that is not.
- **A session stat naming an unknown Metric is dropped, not refused.** The spec
  said validate the slug; refusing would make an Archive from a newer Verve
  unreadable over a decoration. A stat this build cannot name has no canonical
  unit to be in, so it goes, and the workout still arrives. The same situation on
  a Measurement is different and keeps its row: that one has a home in the
  Unmapped bin.
- **The report does not mention the carried profile.** The spec asked for a line
  pointing at the Profile page. `connector.Report` has no field for a note, and
  widening the contract for one sentence is worse than the gap: the Plan page
  already names a missing date of birth in the basis it prints and offers the
  field inline, so an Account that restores onto a fresh instance is told where
  to type it by the one page that needs it.
