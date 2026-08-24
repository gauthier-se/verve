# A Connector reads a file the owner exports

## Context

ADR 0009 settled that Connectors are compiled in with declarative mapping, and
left open what a Connector actually reads. With one Connector the question never
came up: Apple gives you a `.zip` and there is no other way in.

Google is three products, and the name "Google Health" belongs to the one that
used to be Fitbit.

- **The Google Health API** is the former Fitbit Web API on Google OAuth (the
  Fitbit API itself is turned down in September 2026). Every scope is Restricted:
  access requires a privacy and security review by Google, per registered client.
- **Health Connect** is the on-device Android store, exporting its internal
  SQLite database. The schema is an AOSP implementation detail with no
  compatibility promise and no documentation.
- **Google Takeout** exports the Google Health account itself: a `.zip` of CSV
  time series, each with a header, each with a README beside it documenting every
  column and its unit.

The second Connector had to pick one, and the choice generalizes to the third.

## Decision

**A Connector reads a file the owner exports and hands to Verve.** The Google
Health Connector reads a Takeout archive; the Apple Health Connector already read
an export archive. No Connector holds a credential, calls a vendor API, or syncs.

Three reasons, in order of weight:

1. **A self-hosted binary cannot be an OAuth client.** A Restricted scope is
   granted to a reviewed application, not to a program a stranger runs on their
   own machine. Shipping one set of credentials in a public binary is the only way
   to make it work and is not a thing to do. ROADMAP already rules out the hosted
   version that would make it legitimate.
2. **A file is the owner's to give.** It is produced deliberately, it can be
   inspected before it is handed over, and importing it moves nothing off the
   machine. That is the same property as the export path Verve already has, and it
   is the reason people run Verve.
3. **A documented file beats an undocumented database.** Takeout's CSVs describe
   themselves; Health Connect's export is a private schema that can move between
   Android versions. Both are readable, only one can be specified.

**Detection is by content, never by extension.** Both supported exports are
`.zip`. The registry asks each Connector whether it recognizes a path, an
`export.xml` entry for Apple, a `Google Health/` directory for Google, and the
owner is never asked a question their file already answers. Detectors must be
disjoint; a test asserts it.

**An archive is read defensively.** A Connector reads the entries it knows,
keeps in the **Unmapped bin** any health series it has no Metric for (ADR 0002),
and counts the rest as ignored rather than filing account settings and receipts as
measurements. A vendor adding a column, a family or a directory therefore costs an
Account rows in the bin and a line in the report, never a failed import and never a
silent loss.

## Considered Options

- **The exported archive (chosen).** No credentials, no network, no review, and
  the same gesture the owner already knows.
- **The Google Health API.** Live data, incremental sync, no manual export. Not
  available to a self-hosted binary at any price short of a hosted service.
- **Health Connect's exported database.** A real second path to the same data,
  and a better one for an Android-only owner whose phone is the only place their
  history exists. Deferred rather than refused: it is a separate Connector, and
  the Takeout is the better first because its format is documented in the archive
  and the AOSP schema is not documented anywhere.
- **A companion Android app that reads Health Connect and posts to Verve.** Turns
  a health data warehouse into a mobile app project, and needs a Play listing to
  reach anyone.
- **Vendoring a version-pinned copy of the Health Connect schema.** Would make
  the private schema look like a contract without making it one.

## Consequences

- `internal/connector` holds the contract, `internal/connector/registry` the
  compiled-in set. A contributed Connector is a package plus one line in that
  slice, which is the shape ADR 0009 promised and had never been asked to
  deliver.
- The API's upload refusal names what Verve reads, from the registry, rather than
  naming Apple. It stops being wrong the moment a third Connector lands.
- `imports` records the Connector that ran it, so the History can say where an
  archive came from. Two `.zip` files with owner-chosen names are otherwise
  indistinguishable on that page.
- Nothing about identity changes. A Connector may have stable per-record ids from
  its source, Health Connect does, and Google's Takeout does not, and the
  Content key stays Verve's own hash of the row's meaning (ADR 0006), because a
  source that rewrites a value under a new id would otherwise duplicate it.
- Import stays a deliberate act. There is no scheduled sync, so a Verve holding a
  Google Health archive is as current as the last Takeout its owner asked for.
