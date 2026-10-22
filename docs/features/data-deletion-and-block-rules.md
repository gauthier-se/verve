# Deletion and block rules

- **Status:** Planned
- **Milestone:** M1
- **Related:** [Ingestion pipeline](ingestion-pipeline.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Audit log](audit-log.md), [Accounts and administration](accounts-and-administration.md)

## Summary

Users can delete targeted data and block data from coming back. Because Health Auto Export keeps resending recent data and XML re-imports contain the full history, a plain deletion would be undone by the next import. Deleting therefore creates a block rule by default, and block rules can also be created proactively.

## User stories

- As a user, I want to remove an aberrant weigh-in (150 kg) and never see it again.
- As a user, I want to remove everything an old app wrote to my weight, and stop it for good.
- As a user, I want to never store a category of data at all (for example cycle tracking).
- As a user, I want to delete a duplicated workout.

## Scope

### In scope

- Targeted deletion of samples and workouts.
- Block rules at sample, source, type and source plus type levels, with optional time range.
- Block rules management page.

### Out of scope

- Undo of a deletion (backups are the safety net).
- Deleting data in Apple Health (Verve never writes to HealthKit).
- Account deletion (see [Accounts and administration](accounts-and-administration.md)).

## Functional requirements

- **FR-1**: Deletion targets:
  - one or several selected samples (from the metric explorer table),
  - one workout (with its route),
  - all samples of a source, of a type, or of a source and type, optionally restricted to a time range.
- **FR-2**: The deletion dialog shows the number of samples and workouts affected and the time range before confirmation.
- **FR-3**: Option "Block from future imports" is checked by default. When checked, the deletion creates the matching block rule. When unchecked, the data can come back with the next import that contains it.
- **FR-4**: Block rule levels:

  | Level | Matches |
  |---|---|
  | Sample | One record by its natural key or uuid |
  | Source | Every record from a source (or merged source) |
  | Type | Every record of a metric type, any source |
  | Source and type | Records of a type from a given source |

  Every rule can carry an optional time range on the record start.
- **FR-5**: Block rules can be created without deleting anything, from the rules page or from the metric explorer ("Never store this metric"). Creating such a rule asks whether existing matching data should also be deleted.
- **FR-6**: Rules are applied by the ingestion pipeline to every origin (XML, Health Auto Export, native, CLI). Blocked records are counted in import reports.
- **FR-7**: Deletions run as background jobs for large scopes, then trigger aggregate recomputation for the affected types and ranges, as well as dependent computations (sleep nights, records, energy estimates).
- **FR-8**: The rules page lists active rules with level, target, time range, creation date, origin (manual or created by a deletion) and the number of records blocked since creation. Rules can be removed.
- **FR-9**: Removing a rule does not restore data. The UI explains that data will come back only with an import that contains it (for example an XML re-import), since Health Auto Export only resends recent data.
- **FR-10**: Each deletion and rule change is recorded in the [Audit log](audit-log.md) with scope and counts, never with values.

## UX notes

- Delete actions are available contextually: on a sample row, on a workout, on a source in the sources page, on a metric in the explorer.
- Destructive dialogs use explicit wording ("Delete 1,284 weight samples from MyFitnessPal between 2019 and 2021").

## Data and API notes

- `block_rules` table, cached in memory per user and invalidated on change.
- Endpoints: `POST /api/v1/data/delete` (scope description in body), `GET/POST/DELETE /api/v1/block-rules`.

## Edge cases

- A sample rule for a pre-aggregated bucket: matches the bucket key, so a resent bucket stays blocked even if its value changes.
- A source rule on a merged source: applies to all aliases.
- A type rule created while an import of that type is running: applies to the remaining batches, and a cleanup deletes what was inserted before the rule if the user chose to delete existing data.

## Acceptance criteria

- [ ] A deleted weigh-in does not reappear after re-importing the same export.
- [ ] A deletion with "Block from future imports" unchecked is restored by re-importing.
- [ ] Type rules prevent any new sample of that type from being stored.
- [ ] Aggregates, sleep nights, records and energy estimates reflect deletions.

## Future ideas

- Outlier detection that suggests suspicious samples for review (no automatic deletion).
- Deleting everything inserted by a given import.
