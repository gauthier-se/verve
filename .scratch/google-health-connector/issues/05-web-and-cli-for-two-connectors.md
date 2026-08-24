Status: done
Blocked by: 01, 04

# 05: data, api, web: an app that knows it has two Connectors

## What

- **`imports` records which Connector ran** (migration `0014`):
  ```sql
  ALTER TABLE imports ADD COLUMN connector TEXT NOT NULL DEFAULT 'applehealth';
  ```
  A constant default, which is what SQLite allows on a `STRICT` table, and the
  right value for every row already there. `data.Import` (`measurement.go:48`)
  gains `Connector`, `RecordImport` (`:365`) writes it, and each Connector passes
  its own `Name()`: the field is set where the fact is known, not inferred by the
  caller.

- **The History says it.** `history.go:88` selects the column, and the import
  event (`historyhandlers.go:323`) carries it beside the file name so a row can
  read "export.zip, Apple Health" rather than leaving two archives from two
  platforms distinguishable only by whichever name the owner happened to give the
  file. It goes in `Label` as one string rather than as a new view field: the
  History renders events generically, and a `connector` key that only one event
  kind ever sets is a schema change to say a sentence.

- **The import endpoint stops being Apple's.** `handleCreateImport`
  (`importhandlers.go:22`) refuses everything but `.zip` with "only a .zip Apple
  Health export is accepted here". Both exports are `.zip`, so the extension
  check stays and the *message* changes to name both; a zip no Connector accepts
  fails the job with `ErrUnknownExport`'s sentence from issue 01, which is where
  the real refusal now lives.

- **The drop zone names both, and says where the second comes from.**
  `import-page.tsx:265` ("Drop your Apple Health export here") and its validation
  message (`:52`) become format-neutral, with one line of help under the zone:
  Apple Health, Settings, your profile, Export All Health Data; Google Health,
  takeout.google.com, deselect all, keep Google Health. An import page that
  assumes you know how to produce the file is a page for the person who wrote it.

- **The report card names the Connector**, from the same string the History uses,
  so `reportView` (`importhandlers.go`) gains `connector` and `types.ts:116`
  gains it beside `source_file`.

- **The empty states stop saying Apple.** `dashboard-view.tsx:234` ("Import your
  Apple Health export to fill these panels") and `data-page.tsx:301` name both
  sources or neither. Prefer neither, phrased as "import an export": the list of
  supported Connectors is going to grow and these two sentences are not where it
  should be maintained.

- **The CLI already prints the Connector** as of issue 01. Nothing more here
  beyond the `Ignored` block issue 04 adds to `renderReport`.

- **Tests**: `importhandlers_test.go` for the `connector` field on a settled
  job's report and for the refusal message on an unrecognized zip;
  `historyhandlers_test.go` for the import event carrying the Connector;
  `data/measurement_test.go` for the round trip of the new column, including a
  row inserted before the migration reading back as `applehealth`.

## Why here

Everything in this issue is a sentence or a column, and none of it is optional.
Verve is about to hold two archives whose rows can only be told apart by their
Source strings, and the History exists precisely to explain the shape of a
history: an import event that cannot say which platform it came from is the one
place the app would be lying about provenance while a whole page is dedicated to
provenance.

The web copy is grouped here rather than spread through the earlier issues
because it is one editorial pass: four strings that all assume a single source,
written by the same hand on the same afternoon, and best changed by the same one.

## Comments

Shipped as specified. Migration `0014` adds `imports.connector` with the constant
default; `data.Import` carries it and each Connector sets its own `Name()`. The History
import event reads "takeout.zip · Google Health" through `importLabel`, which degrades
to the file alone for a Connector the running binary does not know.

The report card and the upload refusal both name Connectors from the registry rather
than from a literal, so a third one changes no copy. The drop zone, the dashboard empty
state and the Ledger empty state stopped naming Apple; the two empty states name no
source at all, since the list of them is going to grow and those sentences are not
where it should be maintained.
