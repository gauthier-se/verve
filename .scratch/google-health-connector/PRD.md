# PRD: Google Health

## Goal

A second Connector, which is the one thing ROADMAP.md names as the criterion for
dropping the leading zero:

> the criterion for dropping the zero is a second connector having pushed on the
> Catalog without breaking it, not a feature count.

The source is **Google Health**, the product that used to be Fitbit, exported
through **Google Takeout**. A reference export is in hand and every fact below
was read from it rather than from documentation.

Three claims made in ADRs have never met a second source, and each one either
holds here or is wrong:

- **ADR 0009** says a Connector is a Go interface plus a registry with the
  mapping factored out as data. There is no interface and no registry. There is
  a package named `applehealth` imported by name in `cmd/verve/commands.go:185`
  and `internal/api/importjob.go:172`.
- **ADR 0002** says the Catalog is closed but extensible and that a Connector
  maps into it. Every slug in it was chosen by reading an Apple export.
- **ADR 0003** says every Source is kept and overlap resolved at read time. It
  elects **one Source for the whole window**, and the reference export breaks
  that outright: see the concept.

## What the export actually is

`takeout-20260824T115650Z-1-001.zip`, 2.3 MB, 506 entries, under
`Takeout/Google Health/`. Roughly 242,000 data rows covering 2026-05-25 to
2026-08-24.

**The data that matters is fourteen CSV families in one directory**,
`Physical Activity_GoogleData/`, and they share one shape:

```
timestamp,<value column>,data source
2026-08-01T01:15:09.096319Z,12,Apple Health Health Kit
```

The timestamp is RFC 3339, UTC, microsecond precision. The value column names its
own unit (`Kilocalories`, `weight grams`, `height millimeters`, `distance` in
metres per its README, `beats per minute`, `steps`, `body fat percentage`). The
last column is the Source. Each family is sharded across files by day or by
month, and the filename's date is redundant with the timestamp column.

| Family | Rows | Metric |
| --- | --- | --- |
| `active_energy_burned` | 99,770 | `active_energy` |
| `heart_rate` | 88,288 | `heart_rate` |
| `distance` | 32,365 | `distance` (new) |
| `steps` | 21,243 | `steps` |
| `body_fat` | 152 | `body_fat_percentage` |
| `weight` | 152 | `body_mass` |
| `resting_heart_rate`, `daily_resting_heart_rate` | 91 + 91 | `resting_heart_rate` |
| `vo2_max`, `daily_vo2_max` | 85 + 90 | `vo2_max` |
| `height` | 1 | `height` |
| `calories` | 2 | `total_energy_burned` (new) |
| `calories_in_heart_rate_zone`, `daily_heart_rate_zones` | 3 | Unmapped bin |

Two of those pairs are the same data twice inside one export
(`resting_heart_rate` and `daily_resting_heart_rate` hold the same 91 values at
the same instants from the same Source). They need no special case in the
Connector, but they did need one thing settled: Google writes the same reading
`51` in one family and `51.0` in the other, so hashing the cell as the Apple
Connector does keeps both. The **Content key** therefore hashes a *canonical*
rendering of the parsed number, which is just as byte-stable and makes the two
agree. With that, ADR 0006 does exactly what it was written for and the second
copy is skipped.

The rest of the archive is settings, subscriptions, empty feature stubs
(menstrual health, stress, glucose, sleep score), READMEs, and one directory that
does need a decision: `Global Export Data/` holds the legacy Fitbit JSON of the
same quantities, at minute grain, in US units (weight in pounds), dated
`MM/DD/YY` with no timezone and **no source attribution**. It is the same data,
worse, and it goes to the **Unmapped bin** rather than into a Metric.

**There is no sleep, no workout, no route and no nutrition in this export.** The
one sleep artefact is `User3rdPartySleepAttributes.csv`, which lists sleep ids
attributed to "Apple Watch de Gauthier,HEALTH_KIT" and carries no intervals and
no stages. So this Connector produces **Measurements only**, no States, no
Sessions, no Routes, no artifacts, which makes it a remarkably clean test of
whether the Catalog and the Source model generalize, with none of the family
machinery to hide behind.

## The concept

**"Google Health" is three products, and Takeout is the only one that fits.**
The **Google Health API** (the former Fitbit Web API, turned down in September
2026) puts every scope behind a Google privacy review, per registered client,
which a self-hosted binary cannot do and ROADMAP.md rules out anyway.
**Health Connect**, the on-device Android store, exports its private SQLite
database and is a *different* source with a different schema; it stays a separate
future Connector. Takeout is a file the owner asks for and drops into Verve,
which is the gesture Verve already has.

**The Source is an upstream application, and Google Health is only an
aggregator.** CONTEXT.md already says this about Apple ("Apple Health is itself
only an aggregator of upstream sources, never the canonical owner"), and this
export is the proof, because in the reference archive **Google holds almost no
data of its own**:

| `data source` | What it is |
| --- | --- |
| `Apple Health Health Kit` | Apple Health, mirrored into Google over HealthKit |
| `Phone Health Kit` | the iPhone, same path |
| `com.apple.heartratecoordinatord Health Kit` | an Apple daemon, same path, as a raw bundle id |
| `YAZIO Health Kit`, `Zepp Life Health Kit` | the nutrition app and the scale, same path |
| `Google Health App` | the only rows Google itself produced (height, `calories`, `daily_vo2_max`) |

So the honest answer to "what happens when an Account holds Apple and Google" is
not a hypothetical about two ecosystems. **It is the same measurements arriving
twice under different names**, and this is the normal case rather than an edge
one, because HealthKit sync is how most people fill a Google Health account.

**Which is why the read path breaks, today, on this export.** `resolveSource`
(`internal/query/query.go:483`) collects the distinct Sources over the requested
window and elects one for the whole of it. `heart_rate` has no entry in
`sourcePriority`, so the election falls back to alphabetical order, and against
an Apple export whose Source is "Apple Watch de Gauthier" the winner is
**"Apple Health Health Kit"**, the Google mirror, which holds three months.
Import both and a multi-year heart-rate curve silently becomes a three-month one.
Not a regression this milestone introduces: a property of the current rule that
only a second Source can reveal.

The fix is **not** merging, which stays deferred. It is electing at **day** grain
instead of range grain, so a day the mirror covers elects between the two and
every day it does not still comes from Apple. That is not a new mechanic either:
the Manual overlay resolves per day and says why in `sourceFilter`'s own comment
("so a daily chart, a monthly chart and a window summary can never disagree about
which rows are in play"), and sleep resolves per **Night**, which is day grain for
a span that crosses midnight (ADR 0027). Range-grain election is the odd one out.

**Verbatim Source names, and priority patterns that know them.** " Health Kit"
is an ingestion path, not an origin, and it is tempting to strip it. Do not: a
row Google mirrored and a row Apple recorded are different provenance claims, and
rewriting one into the other would make them indistinguishable in storage
forever. The Source string is kept exactly as the export writes it, and
`sourcePriority` learns the names instead, which is the one place where a
judgement about origins belongs and is reversible.

**Nothing about identity changes.** The Content key keeps hashing
`(metric, source, start, end, value, unit)`. The connector does not enter it, so
no existing row moves, and re-importing a Takeout stays idempotent exactly as an
Apple export does. What a Connector passes as the value is its own business, and
this one passes a canonical rendering rather than the raw cell, for the reason
given above.

## What this milestone does

- **`internal/connector` becomes the contract.** `Report`, `Tally`, `Store`,
  `Options`, `Progress` move up out of `applehealth`, joined by a `Connector`
  interface and a registry. This is ADR 0009 being implemented rather than
  restated.
- **The registry detects by content.** Both exports are `.zip`, so extension
  routing cannot tell them apart: an Apple archive holds an `export.xml`, a
  Takeout holds a `Google Health/` directory, and the user is never asked a
  question their file already answers.
- **Source resolution moves to day grain**, with the whole-range predicate kept
  byte for byte whenever one Source wins every day, which is every Account that
  exists today. New ADR.
- **`sourcePriority` learns the real names** from the reference export, so
  `steps` between "Apple Watch de Gauthier", "Phone Health Kit" and
  "Apple Health Health Kit" is decided by a rule rather than by the alphabet.
- **A `googlehealth` Connector** reading the Takeout: the fourteen CSV families
  streamed entry by entry, mapped through a declarative table, written as
  Measurements, with everything it does not map kept in the Unmapped bin.
- **Two new Catalog Metrics**, `distance` and `total_energy_burned`, each one an
  argument rather than a mapping (see issue 03).
- **`imports` carries which Connector ran it** (migration `0014`, default
  `applehealth`), so the History's import events stay honest once there are two.
- **The web import page names both**, and tells the owner where the file comes
  from: takeout.google.com, deselect everything, keep Google Health.

## What this milestone does NOT do

- **No cloud connector.** Not the Google Health API, not Fitbit's. A Restricted
  OAuth scope cannot ship in a self-hosted binary, and a hosted version is a
  ROADMAP "not planned".
- **No Health Connect.** The Android on-device store is a different export with a
  different (private, undocumented, version-drifting) SQLite schema. It is a
  separate Connector and a separate milestone, and this one is the better first
  because its format is documented by the README beside every file.
- **No merging of Sources.** Election stays election; only its grain changes. Two
  Sources that both cover a day still leave one out of that day's series, exactly
  as they leave one out of the range today.
- **No rewriting of Source names.** No stripping of " Health Kit", no collapsing
  of `com.apple.heartratecoordinatord Health Kit` into something prettier. What
  the export says is what is stored; prettifying is a display question and this
  milestone does not answer it.
- **No profile import.** The Takeout carries date of birth, sex, height and
  weight in `Your Profile/Profile.csv`, and Verve's Account has fields for the
  first two. It is also true that **no Connector writes to `accounts` today**,
  the Apple `<Me>` element is not read either, despite CONTEXT.md describing
  those fields as coming from it. Adding that capability means widening the
  Connector's `Store` beyond the families, for both Connectors at once, and it is
  a good next issue rather than a rider on this one.
- **No sleep, Sessions or Routes**, because the export has none. The Connector
  writes one family and the `Store` it is handed still exposes all of them.
- **No `Global Export Data/` as Metrics.** Same quantities, US units, no source
  attribution, `MM/DD/YY` timestamps: it lands in the Unmapped bin, where it is
  kept and inspectable and out of every curve.
- **No `connector` column on `measurements`.** It would either enter the Content
  key, changing every key and duplicating every row on the next import, or sit
  beside it as a field nothing reads. `imports` gets it; the families do not.
- **No cross-Source dedup.** Verve will hold the same heart beat twice, once from
  Apple and once from Google's mirror of Apple. That is ADR 0003 working as
  designed: storage is non-destructive and the read path resolves. An Account
  that finds this wasteful has the tool for it already, which is an **Exclusion**.

## Docs

- **ADR 0034: Source resolution is per day.** Amends ADR 0003's "whole-range
  only" clause. Records the mirrored-Source case as the motivation, with the
  reference export's heart rate as the worked example; day grain rather than
  bucket grain and why; the one-winner fast path; and what was turned down:
  merging complementary Sources, per-bucket resolution, normalizing Source names
  at import, and a user-facing Source picker.
- **ADR 0035: A Connector reads a file the owner exports.** Records why Takeout
  beats the Google Health API for a self-hosted binary, and why detection is by
  content rather than by extension. Names what was turned down: the API, Health
  Connect's private database, and a companion app.
- **CONTEXT.md**: **Source** gains the sentence that a Source may be an
  application and that one Source can be another aggregator's mirror; **Source
  priority** loses "Whole-range only" and gains day grain with its reason;
  **Connector** gains the registry and content detection. No new term: a second
  Connector adding no vocabulary is the outcome to hope for.
- **ROADMAP.md**: a shipped row, and the "Next" paragraph loses the sentence
  about a second connector being what moves the leading zero, because it will
  have moved.

## Issues

1. `01-connector-interface-and-registry`, connector, api, cli: lift the shared
   types into `internal/connector`, add the interface and the registry, detect by
   content, keep the Apple path behaving identically.
2. `02-source-resolution-per-day`, query, catalog, docs: day-grain election, the
   one-winner fast path, the real Source patterns in `sourcePriority`, ADR 0034.
3. `03-catalog-distance-and-total-energy`, catalog: the two new Metrics and the
   argument behind each, and the lock-step test that stops belonging to Apple.
4. `04-google-health-takeout-connector`, connector, cli: the Takeout reader, the
   declarative mapping, the Unmapped bin for the legacy JSON, the report.
5. `05-web-and-cli-for-two-connectors`, web, api, data: the `imports.connector`
   column and its History event, the drop zone that names both exports, and the
   empty states that stop saying Apple.
