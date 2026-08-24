# PRD: Exclusions

## Goal

Two requests, one object.

The first: **delete everything a Metric holds**. On the reference Account
`body_fat_percentage` correlates with `body_mass` at r = 0.9895 over 474 days,
which is a scale reporting a weight lookup rather than a composition
measurement. ADR 0022 gave that case the Manual entry, so a trusted figure can
be typed and win the day it covers. It did not give it a way to say the simpler
thing: this number is not a measurement, take it out.

The second: **choose what an import brings in, and be asked the same thing
next time**. "Everything except body fat, and nothing before 2026" is a decision
made once about an Account's own history, and today it has to be made zero
times because it cannot be made at all.

They are the same feature, and ADR 0022 already explained why, when it rejected
deleting imported rows:

> deleting an imported row removes its key and the next Apple import reinserts
> the row verbatim. The deletion is undone with no warning. Making it stick
> needs a tombstone table consulted at import time, a feature in its own right,
> not a flag on this one.

This is that feature. The tombstone consulted at import time and the remembered
import filter are one table read at one moment, and building either alone
builds the wrong half: a delete that does not stick, or a filter that leaves
behind everything imported before it existed.

## The concept

**An Exclusion is a standing refusal, not a view.** "`body_fat_percentage`, from
2026-01-01" is a statement that a stretch of a Metric is not Verve's to hold. It
does two things, and the pair is what makes it honest: no Connector may write
what it names, and what is already stored under it is deleted when it is
created. It is deliberately **not** a read-time predicate. Hiding rows would
leave the Ledger, the series, the History and the row counts each free to
disagree about what the Account holds, and would answer neither request: the
data would still be there, and the next import would still spend its time on it.

**An Exclusion governs Connectors, never the Account.** A Manual entry on an
excluded Metric is still accepted, and that is the point rather than an
oversight: excluding the scale's body fat and typing the DEXA figure is one
gesture in two halves. What a Connector writes is a stream Verve cannot argue
with; what the owner types is already the argument.

**An Exclusion names a Metric and, optionally, a span of days.** `starts_on` and
`ends_on` are inclusive `YYYY-MM-DD` bounds, either or both absent, the same
shape and the same words an Annotation uses, because both are a period a person
typed rather than a window the server resolved. No bound at all is the whole
history of that Metric, which is the common case and must not require typing two
dates to express.

**The set is the memory.** There is no separate saved import profile. The
Exclusions an Account holds are, by construction, exactly what the next import
will honour, so "when I come back it proposes this import" is the list being
there rather than a second concept that has to be kept in sync with the first.

**Deleting an Exclusion restores nothing by itself.** It reopens the tap, and the
next import brings back whatever the export still holds. That is the recovery
path, and it is worth saying in the interface, because it is also what makes the
purge less frightening than it looks: the export is the backup for everything
except a Manual entry, which is gone for good.

_Avoid_: Filter (a filter selects a view of what is there; an Exclusion means it
is not there), Rule (both `Baseline rule` and the aggregation rule already claim
the word), Import profile or Preset (Preset is the Time range's word, and a
profile implies several sets to switch between), Blocklist, Ignore, Mute (all
imply the data is present and unheard), Tombstone (the mechanic ADR 0022 named,
not the thing the owner creates).

## What this milestone does

- **An Exclusion is Account data.** New table `exclusions (id, account_id,
  metric, starts_on, ends_on, purged, created_at)`, migration `0013`. Unbounded
  ends are the empty string rather than `NULL`, so `UNIQUE (account_id, metric,
  starts_on, ends_on)` actually catches a duplicate and the predicates keep the
  `? = '' OR ...` shape `ListManual` already uses.
- **Creating one purges what matches**, in the same transaction that writes it,
  and the row keeps the count it removed. A purge covers every Source including
  `Manual`, because "delete every body fat value" means every one; the
  confirmation says so with the count before it runs.
- **Three endpoints**: `GET /v1/exclusions`, `POST /v1/exclusions` (returning the
  row and how much it purged), `DELETE /v1/exclusions/{id}`. No `PATCH`: widening
  a span has to purge again, which is what creating one already does, so editing
  is delete then create and there is one code path that removes data.
- **The Connector honours the set.** `applehealth.Import` takes an `Options`
  carrying the Exclusions, and a Record whose Metric and day match one is dropped
  before dedup rather than written. The check is a map lookup keyed by slug, on a
  path that runs a few hundred thousand times, and it costs nothing for the
  Account that has no Exclusion.
- **An excluded record is counted, never hidden.** `Report` gains `Excluded` and
  the per-Metric `Tally` gains its column, so the CLI report and the web report
  card both say how much was refused and under which Metric. An import that
  quietly drops data is the failure mode this feature has to avoid being.
- **An excluded record does not fall into the Unmapped bin.** The bin exists so
  no source data is lost to a Catalog gap (ADR 0002); routing a deliberate
  refusal there would keep, row for row, exactly what was asked to be dropped.
- **The Import page carries the list**, above the drop zone: what will be
  excluded from the import you are about to run, editable there, with an
  add-exclusion control over the Catalog's Metrics. This is the whole of "when I
  come back, it proposes this import".
- **The Metric page carries the delete.** A destructive action, its confirmation
  naming the Metric, the span, and the number of Measurements about to go, and
  stating that the Exclusion it creates is what keeps them from coming back.
- **The History gains an `exclusion` event**: what was excluded, when, and how
  much it removed. A purge is a dated fact about the shape of the data, and the
  History is where the shape is explained (ADR 0032).
- **The CLI honours it too.** `verve import` reads the same table for the same
  Account, so the two import paths cannot diverge on what they accept.

## What this milestone does NOT do

- **No exclusion of States or Sessions.** `sleep` is stored as States and a
  workout as a Session, each with its own table and its own key, and covering
  all three triples the surface for a case nobody has asked for yet. A `sleep`
  or Session slug is refused with a message that says so rather than accepted
  and silently ignored, which is the only outcome worth avoiding here.
- **No exclusion of a derived Metric.** A derived Metric owns no rows (ADR 0014);
  its operands do. Excluding `calorie_balance` would have to mean excluding
  `dietary_energy`, which the Account can say itself, and more precisely.
- **No Source axis.** `(metric, source)` reads as the sharper tool for the
  untrustworthy-scale case, and it is not: the scale is the only thing reporting
  body fat, so the Metric already names it, and a Source-scoped Exclusion invites
  the per-Source questions Source priority (ADR 0003) exists to answer at read
  time. It stays a deferrable axis, in one direction only.
- **No per-run override.** The list applies to every import, and there is no
  "just this once" checkbox on the drop zone. Two mechanisms that can disagree
  about what an import did is exactly one more than this feature needs, and a
  one-off exclusion is a standing one plus a delete.
- **No read-time hiding.** See the concept. An Exclusion removes.
- **No undo.** Deleting an Exclusion and re-importing is the restore path for
  imported rows; a Manual entry is not restorable and the confirmation says so.
- **No exclusion of the Unmapped bin's contents.** The bin holds types the
  Catalog does not know, so there is no Metric to name in an Exclusion. Pruning
  it is a different feature with a different key.
- **No blocking of Manual entries.** An Exclusion is about what a Connector may
  write. `POST /v1/measurements` is untouched.

## Docs

- **A new ADR**: an Exclusion is a write-time refusal plus a purge, one object
  serving both the delete and the import filter, honoured by every Connector and
  never by a read path. It is the direct sequel to ADR 0022's rejected option, so
  it names it, and records what was turned down here: read-time hiding, a
  separate saved import profile beside the exclusion set, a `PATCH` that edits a
  span without re-purging, a Source axis, routing excluded records to the
  Unmapped bin, and blocking Manual entries on an excluded Metric.
- **A CONTEXT.md entry** for **Exclusion** with its `_Avoid_` list, in the
  cross-cutting section next to **Import** and **Content key**, since it is a
  property of the write path and of no data family in particular.
- **ROADMAP.md**: a shipped row.

## Issues

1. `01-exclusion-model-purge-and-api`, data and api: the migration, the store,
   the purge, the three endpoints, the History event, the ADR and the CONTEXT.md
   entry.
2. `02-connector-honours-exclusions`, connector and cli: `applehealth.Options`,
   the per-Record predicate, the `Excluded` counts in the Report, both call
   sites.
3. `03-web-exclusions-and-metric-purge`, web: the list on the Import page, the
   destructive action and its confirmation on the Metric page, the excluded
   count on the report card, the History event's row.
