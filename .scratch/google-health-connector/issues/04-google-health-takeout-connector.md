Status: done
Blocked by: 01, 03

# 04: connector, cli: read a Google Takeout, write Measurements

## What

A new package `internal/connector/googlehealth`, registered in the registry from
issue 01. It writes **one family**, Measurements, because the export contains
nothing else.

- **`Accepts`** is a `.zip` holding an entry with a `Google Health/` path
  segment. Match the segment, not a `Takeout/` prefix: the archive's root folder
  is Google's to name, a Takeout can bundle a dozen other services beside this
  one, and the segment is what identifies the part we read. Disjoint from
  `applehealth.Accepts` by construction, and a test asserts it against the Apple
  fixture.

- **The mapping is a table** (ADR 0009), keyed by *family*, which is a filename
  with its date shard removed (`heart_rate_2026-08-20.csv` and the 65 files
  beside it are one family):
  ```go
  // csvMetric maps one Physical Activity family to a Catalog Metric. column is the
  // header cell carrying the value, unit what that column is in, both read from the
  // README beside the file rather than guessed.
  type csvMetric struct{ metric, column, unit string }

  var familyToMetric = map[string]csvMetric{
      "active_energy_burned":     {"active_energy", "Kilocalories", "kcal"},
      "steps":                    {"steps", "steps", "count"},
      "distance":                 {"distance", "distance", "m"},
      "heart_rate":               {"heart_rate", "beats per minute", "count/min"},
      "resting_heart_rate":       {"resting_heart_rate", "beats per minute", "count/min"},
      "daily_resting_heart_rate": {"resting_heart_rate", "beats per minute", "count/min"},
      "weight":                   {"body_mass", "weight grams", "g"},
      "height":                   {"height", "height millimeters", "mm"},
      "body_fat":                 {"body_fat_percentage", "body fat percentage", "%"},
      "vo2_max":                  {"vo2_max", "vo2 max value", "mL/min·kg"},
      "daily_vo2_max":            {"vo2_max", "daily vo2 max value", "mL/min·kg"},
      "calories":                 {"total_energy_burned", "calories", "kcal"},
  }
  ```
  Every conversion is either the identity or a factor already in
  `internal/units`; this Connector adds no unit.

- **Columns are found by header name, never by index.** Every file carries its
  header, and it is the only self-description the format has. `vo2_max.csv` is
  the reason this matters: its columns are
  `timestamp,vo2 max value,vo2 max error,measurement method,data source`, and a
  connector reading "column 1" imports an error rate as a fitness score the day
  Google adds a column. A family whose header lacks its mapped column is an error
  naming both, not a silent skip.

- **Time is truncated to the second.** Google writes microseconds
  (`2026-08-01T01:15:09.096319Z`); store `t.UTC().Format(time.RFC3339)`, which is
  byte-for-byte what `applehealth.normalizeTime` (`import.go:516`) produces, so
  both Connectors' rows sort, bucket and compare identically. The precision is
  not lost to carelessness: the API serves day grain at the finest (ADR 0012), and
  two samples in one second either differ in value, in which case both are kept,
  or are identical, in which case one is a duplicate and the Content key was
  going to collapse them anyway.

- **`start_at` equals `end_at`.** A Google row is an instant where an Apple row is
  an interval. Bucketing reads `start_at` only, so nothing downstream cares, and
  claiming a duration the export does not state would be inventing one.

- **The Source is the last column, verbatim.** `Apple Health Health Kit`,
  `Phone Health Kit`, `com.apple.heartratecoordinatord Health Kit`,
  `YAZIO Health Kit`, `Zepp Life Health Kit`, `Google Health App`. No stripping,
  no prettifying, no normalizing: see the PRD. A row Google mirrored and a row
  Apple recorded are different provenance claims and must stay distinguishable in
  storage.

- **The Content key is the existing one**, over the raw CSV value string exactly
  as it appears (`0.16699999999999998`), which is what makes the reference
  export's two copies of resting heart rate collapse into one stored row without
  a line of code: same metric, same source, same instant, same string, same hash
  (ADR 0006).

- **The Unmapped bin takes what has no Metric, and only that.**
  - Unmapped families under `Physical Activity_GoogleData/`
    (`calories_in_heart_rate_zone`, `daily_heart_rate_zones`, and anything Google
    adds later): one row per record, `source_type` = `google_health/<family>`,
    `value` = the row as a JSON object of header to cell, `unit` empty, times and
    Source read as usual.
  - `Global Export Data/*.json`: the legacy Fitbit series, same quantities in US
    units with `MM/DD/YY` timestamps and no source attribution.
    `source_type` = `google_health/global_export/<stem>`, `value` = the compacted
    JSON element, Source = `Google Health`. Its timestamps are parsed as UTC with
    a comment saying that is a guess, which costs nothing because the bin is never
    graphed (ADR 0012 serves no raw rows).
  - **Everything else in the archive is skipped, and counted.** Account settings,
    subscriptions, notification preferences, the avatar and the READMEs are not
    health records, and binning them would file a Premium receipt as a
    measurement. `Report` gains `Ignored map[string]int` keyed by directory, and
    `renderReport` prints it under "Ignored (not health data)". Silence is the one
    thing an import must not do (ADR 0033).

- **Streaming, entry by entry.** `encoding/csv` with `FieldsPerRecord = -1`,
  read row by row rather than `ReadAll`; entries sorted by name so the report is
  deterministic; families flushed at `connector.batchSize`. Peak memory is one
  batch, as with Apple, even though the whole archive is 2.3 MB. Files holding
  the literal `no data`, or a header and nothing else, are skipped without an
  error: the reference export has 232 of them.

- **Progress** reports bytes: total is the summed uncompressed size of the entries
  the Connector will read, decoded the bytes consumed. Same contract as the XML
  path, so the web import's second phase needs no change.

- **Exclusions** are honoured through the shared `Options`, checked after the slug
  is known and before the batch append, counted into `Excluded` and the
  per-Metric `Tally`, exactly as `applehealth` does it.

- **Tests** with a fixture zip built in the test (there is no need to check a
  2.3 MB archive into the repo, and no wish to check a real one in at all):
  three mapped families, one unmapped family, one legacy JSON, one settings file;
  asserting that 93400 grams becomes 93.4 kg and 8.80 metres 0.0088 km; that
  `resting_heart_rate` and `daily_resting_heart_rate` holding the same instant,
  value and Source produce **one** row and one skip; that the unmapped family
  lands in the bin with its JSON; that the settings file is ignored and counted;
  that a header missing its mapped column fails loudly; that a second import adds
  nothing; that an Exclusion refuses and is counted. Plus
  `TestMappingTargetsExist` over `familyToMetric`, the forward half of what issue
  03 split.

## Why here

This is the smallest honest second Connector, and its smallness is the point.
There is no sleep, no workout, no route and no artifact in the export, so the
whole of it is "read rows, map, write Measurements", which means every problem it
hits is a problem of the *contract* rather than of a family. If a second
Connector that writes one family still needs to touch the query engine, the
Catalog and the Report, then those were the places that were still Apple's, and
finding that out is worth more than the fourteen CSV families.

Reading the README beside every file rather than inferring units from the numbers
is not diligence, it is the difference between 93.4 kg and 93,400 kg. Google
documents each column in the archive itself, which is more than Apple does, and
it is the reason this Connector can be specified precisely and the Health Connect
one cannot.

## Comments

Shipped, and verified against the real 2.3 MB reference Takeout rather than only
against the fixture: 242,239 Measurements and 2,766 Unmapped rows in about five
seconds, and a second run of the same archive adds nothing.

Two things the spec got wrong, both found by running it:

- **The directory is spelled with a non-breaking space.** Every entry in the reference
  archive reads `Takeout/Google Health/...`, so `strings.Contains(name, "/Google
  Health/")` matched nothing and the Connector declined its own export. This is the
  same trap `internal/catalog/priority.go` documents for Apple's device names, from a
  different vendor: a name that looks right on screen and is not the bytes it appears
  to be. Entry names are normalized before any comparison, which removes the class
  rather than the instance.
- **The Content key had to hash a canonical value, not the raw cell.** This issue said
  the two copies of resting heart rate would collapse "without a line of code". They
  did not: Google writes `51` in one family and `51.0` in the other, so the raw strings
  differ and the import produced 182 rows for 91 readings. `canonical()` renders the
  parsed float as the shortest string that reads back to it, which is derived rather
  than transcribed and so just as byte-stable as ADR 0006 wants. The Apple Connector is
  untouched, so no existing key moves. With it: 91 added, 91 skipped.

The rest matched: columns by header name, instants rather than intervals, Sources
verbatim, the legacy JSON binned, settings ignored and counted. `Report.Ignored` and its
"Ignored (not health data)" block in the CLI report exist as specified, and on the
reference archive they account for every one of the 232 empty Biometrics placeholders,
the 14 READMEs and the account material, so nothing in the archive is unaccounted for.
