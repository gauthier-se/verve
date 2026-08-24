Status: done

# 03: catalog: two Metrics a second source needs, and one test that stops being Apple's

## What

Twelve of the fourteen CSV families in the reference Takeout land on slugs that
already exist: `weight` on `body_mass`, `body_fat` on `body_fat_percentage`,
`active_energy_burned` on `active_energy`, `heart_rate`, `resting_heart_rate`,
`vo2_max`, `steps`, `height`. The Catalog was seeded broadly on purpose
(ADR 0011) and it shows. Two do not, and each is a decision rather than a
mapping.

### `distance` (km, `Sum`), because Google does not split it

Apple has `DistanceWalkingRunning`, `DistanceCycling`, `DistanceSwimming`.
Google Health has one `distance` family, in metres, with no activity anywhere
near it: the reference export's 32,365 rows say only "8.80 metres at
01:15:09.096319Z, from Apple Health Health Kit". Mapping that onto
`distance_walking_running` would write a guess into stored data, and the guess is
wrong for every ride. A neutral `distance` says exactly what the source says.

```go
{"distance", "km", Sum},
```

The cost is real and should be stated rather than hidden: an Account holding both
exports has its walking distance under two slugs and no Panel adds them up. That
is the honest shape of the underlying disagreement. Inventing a merge here would
be inventing data, and the tool for an Account that wants only one of them
already exists and is an **Exclusion**.

### `total_energy_burned` (kcal, `Sum`), kept apart from `total_energy_expenditure`

Google's `calories` family is "number of calories burned", total rather than
active, produced by the Google Health App itself. `total_energy_expenditure` is
the **derived** Metric Verve computes as `active_energy + basal_energy`
(ADR 0014). They are different claims about the same quantity and they routinely
disagree, which is the same distinction CONTEXT.md already draws between
`basal_energy` and the **Basal estimate**: conflating them would make the
disagreement invisible.

```go
{"total_energy_burned", "kcal", Sum},
```

The uncomfortable part is that importing it invites an Account to put both on one
Panel and read a total twice. The alternative, binning it, is worse: a
Google-native Account has `calories` and neither of the two operands
`total_energy_expenditure` needs, so refusing it would leave that Account with no
energy figure at all while Verve claims no source data is ever lost. Import it,
name it distinctly, let the Metric page say what it is. **This is the one Catalog
decision in this milestone worth a second opinion before it ships**, because a
slug is forever in stored rows. The reference export has two rows of it, which is
the good moment to get it wrong cheaply.

### Nothing else, deliberately

`calories_in_heart_rate_zone` is a categorical breakdown of energy by zone and
`daily_heart_rate_zones` is a zone *definition*, not a measurement. Neither is a
scalar of a canonical kind, so both go to the Unmapped bin (issue 04) rather than
inventing a Metric shape for three rows. No units are added either: metres,
grams and millimetres are all in `internal/units` already, and the conversions to
km, kg and cm are the identity path the table was written for.

### The lock-step test has to change, and that is the milestone's real assertion

`TestMappingMatchesCatalog` (`applehealth/import_test.go:252`) asserts both
directions, and the second is now false by construction:

```go
t.Errorf("imported Catalog metric %q has no Apple mapping", slug)
```

`distance` has no Apple mapping and never will. Move the reverse direction into a
Connector-agnostic test asserting that **every imported Catalog Metric is claimed
by at least one registered Connector**, with each Connector exposing its mapped
slugs for the purpose; keep the forward direction ("every mapping target is a
real slug") inside each Connector's package, where it belongs. The invariant
ADR 0009 wants is that the Catalog has no orphan, not that Apple owns everything.

Add the two Metrics to `metric-icon.tsx` while the slugs are fresh: an unmapped
slug there falls back silently, which is exactly the kind of missing polish that
survives three milestones.

## Why here

This issue is what ROADMAP.md means by "a second connector having pushed on the
Catalog without breaking it". The push is not the two rows, which are cheap. It
is that one of them exists because a second source refuses to answer a question
Apple answers, the other exists because two sources answer the same question
differently, and one test whose assertion was quietly "the Catalog is Apple's" is
about to become "the Catalog is Verve's".

It lands before the Connector so the mapping table has somewhere to point, and so
the decisions are taken in a diff where they are the subject rather than three
lines inside a CSV reader.

## Comments

Shipped. Two Metrics, `distance` and `total_energy_burned`, both with the argument in
the comment beside them, and the coverage test moved.

The split landed as described: `applehealth`'s `TestMappingMatchesCatalog` became
`TestMappingTargetsExist` (forward only), and `registry.TestCatalogHasNoOrphan` asserts
that every imported Catalog Metric is claimed by some Connector, asking each package's
new `MappedSlugs()`. Writing it turned up a detail the issue had wrong: the reverse
assertion also covered `duration_by_state` Metrics through Apple's State kinds, and
`stand` is a State kind that is deliberately *not* a Metric. So the forward test does
not assert State kinds at all, and `TestClaimedSlugsAreKnown` names `stand` as the one
claimed slug that is a stored State kind rather than a Catalog entry.

No units were added. The reference export turned out to need only metres, grams and
millimetres, all already in `internal/units`, so the power, pressure and glucose
entries this issue anticipated were for a source Verve does not read yet.
