Status: done

# 01: connector, api, cli: a Connector is a contract, not a package

## What

No behaviour changes in this issue. It is the refactor that makes issue 04 an
addition rather than a second wiring, and it should be reviewable as "the Apple
import does exactly what it did".

- **`internal/connector` holds the shared types.** Move out of
  `internal/connector/applehealth/import.go`, unchanged in shape:
  `Store` (`import.go:37`), `Tally` (`:50`), `Report` (`:57`),
  `Progress` (`:83`), `Options` (`:89`). They are already written as
  Connector-facing types rather than Apple-facing ones (`Options`'s own comment
  says so), which is why this is a move and not a redesign. `batchSize`
  (`:28`) moves too: it bounds a write transaction, which is a property of the
  store and not of XML.
  `applehealth` keeps `appleTimeLayout`, the zip and XML plumbing, and every
  `classify*` / `build*` function.

- **The interface.**
  ```go
  // Connector reads an external export and writes it into the canonical families
  // (ADR 0009). Implementations are compiled in and registered in this package.
  type Connector interface {
      // Name is the stable identifier recorded with an Import, e.g. "applehealth".
      Name() string
      // Accepts reports whether path is an export this Connector can read. It is
      // asked before Import and must be cheap: open, look, close.
      Accepts(path string) bool
      Import(ctx context.Context, store Store, accountID int64, path string, opts Options) (Report, error)
  }
  ```
  `Accepts` takes a path rather than a reader because both formats are archives
  needing random access, and neither can be sniffed from a prefix.

- **The registry, and detection by content.**
  ```go
  // Registered returns the compiled-in Connectors in registration order.
  func Registered() []Connector
  // For returns the Connector that accepts path, or ErrUnknownExport. The first
  // to accept wins; the two shipped detectors are disjoint by construction.
  func For(path string) (Connector, error)
  ```
  `applehealth.Accepts` is the existing `findExportXML` (`import.go:160`) over
  the archive, plus the bare-XML path it already supports: a `.zip` holding an
  `export.xml` entry, or a file whose first element is `<HealthData>`.
  `googlehealth.Accepts` (issue 04) is a `.zip` holding a `Google Health/`
  directory. Detection by content is the point. Both exports are `.zip` files, so
  extension routing cannot tell them apart, and asking the user which one they
  have is asking them a question their file already answers.

- **`ErrUnknownExport`** is a sentinel in `internal/connector`, so
  `humanImportError` (`importjob.go:234`) can turn it into a sentence naming both
  supported exports instead of a stack-shaped message.

- **Both call sites take a Connector.**
  - CLI (`cmd/verve/commands.go:185`): `connector.For(path)` before the import,
    and the error names what was expected. `renderReport` (`:199`) and
    `renderFamily` (`:249`) take `connector.Report` / `connector.Tally`. Add the
    Connector's name to the report header, which is one word and the only visible
    change in this issue:
    ```
    Imported export.zip (applehealth)
    ```
  - Web (`internal/api/importjob.go`): `importRegistry.store` becomes
    `connector.Store` (`:71`), `importJob.report` becomes `*connector.Report`
    (`:61`), and `run` (`:147`) resolves the Connector from `tmpPath` before
    calling it (`:172`). A file that no Connector accepts fails the job with the
    human message rather than reaching an import at all.

- **`tempPath` keeps the `.zip` suffix** (`importjob.go:130`) but its comment
  changes: the extension no longer decides how the file is opened, it is only a
  hint for anyone looking in the temp dir. `applehealth.Import` still opens by
  extension internally, which stays correct because it is only ever called with a
  path its own `Accepts` returned true for.

- **Tests.** `internal/connector/registry_test.go`: the Apple fixture is
  accepted by `applehealth` and by nothing else; a zip holding neither marker is
  `ErrUnknownExport`; a directory and a missing file return the same error rather
  than panicking. Everything in `internal/connector/applehealth/` keeps passing
  with the types renamed, which is the actual assertion of this issue.

## Why here

ADR 0009 has been the design for eleven milestones and has never been code:
`applehealth` is imported by name in the CLI and in the API, its `Report` is the
API's wire shape, and its `Store` is what `data.ImportStore` is written against
(`models.go:52`, whose comment already says "kept here, not in that package, to
avoid an import cycle"). Every one of those is a decision that belongs to
`internal/connector` and currently belongs to a vendor.

Doing this as its own issue, with no behaviour change, is what keeps issue 04
honest. A new Connector written against a wired-in package would either copy the
wiring or bend it, and in both cases the second Connector would be the one
paying for the first one's shortcuts. The community contribution ADR 0009 asks
for is a package plus one registry line, or it is not a contribution.

## Comments

Shipped. `internal/connector` holds `Store`, `Options`, `Progress`, `Report`, `Tally`
and `BatchSize`; both Connectors implement one interface; the CLI and the web import
resolve one from the file. The Apple package kept its decoder and every one of its
tests, which is the assertion this issue was for.

Three things the spec did not anticipate:

- **The registry needed its own package.** `internal/connector` cannot hold the
  registry and be imported by the implementations: that is an import cycle. The
  alternatives were self-registration in `init()` with blank imports at the call
  sites, or a package that points up. `internal/connector/registry` points up, which
  suits a codebase with no globals and no wiring magic. The types point down, the
  registry points up, and a contributed Connector is a package plus one line in one
  slice.
- **The interface grew a `Label()`.** `Name()` is `applehealth`, recorded with the
  Import and matched on; `Label()` is "Apple Health", which is what a refusal message
  and a report card have to say. Deriving one from the other would have been a
  prettifier in the API layer, guessing at strings a Connector already knows.
- **`Report` grew a `Connector` field.** The CLI prints it in the header, the API
  carries it on the report card, and the Connector sets it where the fact is known
  rather than having each caller attach what it thinks it just ran.

One behaviour did change, deliberately: a `.zip` holding neither export now fails with
"no connector recognizes this archive. Verve reads Apple Health and Google Health
exports" instead of "no export.xml inside the archive". `TestImportMissingExportXMLFails`
became `TestImportUnrecognizedArchiveFails`, because naming one Connector's marker file
is an answer to a question the owner did not ask.
