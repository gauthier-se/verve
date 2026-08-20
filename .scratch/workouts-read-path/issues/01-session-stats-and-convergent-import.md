Status: done

# 01: data, connector: every workout statistic kept, and a re-import that converges

## What

- **Migration `00NN_session_stats.sql`** (next free number). One table, keyed by
  the aggregate Apple actually reports:

  ```sql
  CREATE TABLE session_stats (
      session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
      metric     TEXT NOT NULL,  -- canonical Catalog slug (heart_rate, steps…)
      stat       TEXT NOT NULL,  -- sum | average | min | max
      value      REAL NOT NULL,  -- Catalog unit for metric
      PRIMARY KEY (session_id, metric, stat)
  ) STRICT;
  ```

  No `account_id`: a stat is reachable only through its Session, which is owned
  (ADR 0007), and the cascade means a Session's stats cannot outlive it. The
  primary key is the dedup identity, so re-recording a stat is an upsert rather
  than a duplicate, which is what makes the convergent import below cheap.
  Header comment in the register of `0003_states_sessions.sql`: what a stat is,
  why the four aggregates are kept rather than one, why the units are Catalog
  units, and the pointer to ADR 0011.

- **`addStatistic` stops choosing** (`internal/connector/applehealth/workout.go:93`).
  Today it switches on two Apple types and reads only `sum`. It should:
  1. map the statistic's `type` through the existing `typeToMetric`
     (`mapping.go`) — an unmapped type is skipped, exactly as an unmapped Record
     is, and lands nowhere rather than in a second unmapped bin;
  2. read every attribute present among `sum`, `average`, `minimum`, `maximum`;
  3. convert each through `units.Convert` into the Catalog unit for that slug
     (`catalog.Lookup(slug).Unit`), skipping a value whose unit will not
     convert rather than storing a number in an unknown unit;
  4. accumulate them on the builder as `[]data.SessionStat`.

  The two promoted columns keep being written exactly as today: distance and
  active-energy sums still land in `total_distance` and `total_energy`. They are
  now *also* rows in `session_stats`, and that duplication is deliberate — the
  list sorts and displays from the columns without a join. Say so in a comment,
  or someone will "fix" it.

- **`SessionStat`** in `internal/data/session.go`, beside `Session` and `Route`,
  and `InsertSessionStats(ctx, sessionID, stats)` doing one
  `INSERT ... ON CONFLICT DO UPDATE SET value = excluded.value` per row. Update
  rather than ignore: a corrected export should correct the stored figure, and
  the alternative is a stat that can never be fixed.

- **The convergent re-import.** `InsertSession` already recovers the existing id
  when the workout was seen before (`session.go:60`) — the caller in
  `import.go` must attach stats and routes on *both* branches, not only on the
  newly-inserted one. Without this, widening the ingestion is retroactively
  useless: every workout already in the database stays stat-less forever, and
  the README's painless re-import is a false claim. Check the current route
  attachment on the same branch while you are there.

- **`Report.PerActivity`** already tallies Sessions (`import.go:70`). A workout
  that was skipped but gained stats is not "new", so leave the tally alone;
  the tally counts Sessions, and no Session was created.

- **Tests**:
  - a workout with four `WorkoutStatistics` types, one of them unmapped, stores
    a row per (metric, stat) for the mapped ones and nothing for the unmapped;
  - a statistic carrying `average`, `minimum` and `maximum` but no `sum` stores
    three rows (the case the current code drops entirely);
  - unit conversion: a distance statistic in metres lands in km, an energy in
    joules lands in kcal, matching `catalog.Lookup`;
  - **the convergence test, which is the point of this issue**: import an
    export, then import the same export again with the statistics present, and
    assert the pre-existing Session now carries them and was not duplicated;
  - a second import of a *corrected* value overwrites rather than duplicating;
  - deleting a Session removes its stats (the cascade);
  - cross-Account isolation is unchanged: a stat is reachable only via an owned
    Session.

- **Docs**: `docs/adr/0028-sessions-are-entities-routes-are-resources.md` per the
  PRD's Docs section — both decisions and every rejected alternative — and the
  **Activity**, **Session stat** and **Route** entries in CONTEXT.md, plus the
  line on the existing **Session** entry (`CONTEXT.md:47`) noting the family is
  now read and that the interface says Workouts where the domain says Session.

## Why here

The temptation is to treat this as a read-path milestone and leave ingestion
alone, on the grounds that ADR 0011 says the data is already captured broadly
enough to display retroactively. For this family that is simply not true, and
the detail view is what makes it visible: two statistics out of the dozen a
workout carries survive the import today. Fixing it here, before anything reads
a Session, is the difference between one migration and a second one later that
has to reason about what the first one already displayed.

Keying on `(session_id, metric, stat)` rather than flattening to one value per
metric is the same instinct: Apple reports an average heart rate and a maximum
heart rate, and collapsing them loses the one people actually look at without
anyone deciding to lose it.

The convergence fix is small and easy to skip, and skipping it silently voids
the whole issue for every database that already exists — which is every
database. It deserves its own test with its own name.

## Comments

Three departures from the spec above, each found while writing it out.

**The routes half of the convergence was already correct.** `finishWorkout`
already ran its route loop on both branches (`import.go`), so only the stats
needed attaching there. The issue said to check; the check found nothing to fix,
which is worth recording so the next reader does not go looking.

**The promoted columns keep their own conversion, and that is a bug this issue
nearly introduced.** Converting a statistic once into its Catalog unit and
promoting that value looks obviously right and is wrong for swimming:
`distance_swimming` is canonically metres while `sessions.total_distance` is
documented in km, so a 1500 m swim would have been stored as a 1500 km ride. The
promotion now converts from the raw value into the column's own unit, and
`TestPromotedColumnsUseTheirOwnUnit` stands over it.

**`min` and `max`, not `minimum` and `maximum`.** Apple's attributes are spelled
in full; the stored `stat` values are the short forms, matching the vocabulary
`internal/query` already uses for a Point's min/max band. The parse maps between
them in one place.

Verified by mutation: attaching the stats only on the newly-inserted branch
fails `TestReimportConverges` and `TestReimportCorrectsAValue`, which is exactly
the regression that would make the widening useless for every database that
already exists. `make ci` is green.
