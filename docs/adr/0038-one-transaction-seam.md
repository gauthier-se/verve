# One transaction seam, and a workout is one write

## Context

`internal/data` had ten hand-rolled transactions. Each spelled the same four lines
of `BeginTx`, deferred `Rollback` and `Commit`, with its own wrap string, and the
three batch inserters (measurements, unmapped records, states) were the same forty
lines three times over, differing only in their columns and in the noun in their
error messages.

The cost was not the repetition. It was that **the seam was unexported**. `querier`
was a private interface over `QueryRowContext` and `ExecContext`, and the handful
of functions taking one were private too, so no caller outside `internal/data`
could make two writes atomic. `Models.CreateAccount` exists for exactly that
reason: seeding a Dashboard alongside an Account needed one transaction, so it
became a bespoke method on `Models`. Every future cross-family write would have
needed another one.

The bill came due on the workout write. A Session, the summary stats it carries and
the Routes attached to it were three separate calls, each in its own transaction or
in none: `InsertSessionStats` had no transaction at all until recently, so a failure
partway left a Session carrying some of its figures and not others, which nothing
downstream can distinguish from a workout that genuinely recorded only those. Even
with that one fixed, a crash between the three calls could leave a Session with no
stats, or a route file copied to disk that no row pointed at.

## Decision

**One exported seam: `Models.Tx`.** It runs a callback inside one transaction and
commits, or rolls back and returns the callback's error.

**It hands back a bound `Models`, not a bare `*sql.Tx`.** Verve caps the pool at a
single connection (`Open`), because SQLite tolerates one writer. A transaction
holds that connection, so any statement issued against the pool while one is open
waits for a connection that cannot be released until the transaction ends: a
deadlock, not an error. A caller holding only the tx-bound `Models` has nothing to
reach the pool with, so the mistake is unrepresentable rather than reviewed.

**Models run against a `Handle`**, an interface satisfied by both `*sql.DB` and
`*sql.Tx`. One model, unchanged, serves a direct write and a write inside a
transaction.

**`Tx` and its single-model twin `atomically` are reentrant.** Called on an
already-bound handle they run inline rather than starting a second transaction.
That is what lets `InsertBatch` keep its own atomicity when called directly and
still join the unit when a caller wraps several writes together.

**A workout is one write.** `SessionModel.InsertWorkout` takes the Session, its
stats and its Routes and writes them as one unit, and `connector.Store` asks for it
as one call in place of the three it had.

## Why

- **The deletion test.** Delete `Tx` and the complexity reappears at every caller
  that needs two writes to agree, as a bespoke `Models` method each time. It
  concentrates.
- **The unrepresentable-mistake argument beats the documented one.** The
  single-connection deadlock cannot be caught by a test, only by a reviewer
  noticing. Binding the `Models` removes the way to make it.
- **Reentrance is what makes one seam enough.** Without it there would be two ways
  to write every row, a plain one and a tx-taking one, and the second would be the
  one a contributor forgets exists.
- **A workout is one thing an Account did.** Three coordinated writes was a
  storage detail leaking into the shape of a domain object, and the Session's id
  was the only reason for the order. It still is, but inside a transaction rather
  than across three.

## Considered Options

- **Export `querier` and leave the ten transactions in place.** Smaller. Rejected:
  it exports the hazard without the guard, since a caller with a `*sql.Tx` and a
  `Models` in scope can still mix them and deadlock.
- **A `WithTx(fn func(*sql.Tx) error)` helper.** The usual Go shape. Rejected for
  the same reason: the callback receives a handle but the models it would want to
  call are still bound to the pool, so every write inside would have to be a
  tx-taking variant.
- **Keep the three workout calls and wrap them in `Tx` at the call site.** Would
  work, but it puts a storage concern in the Connector and requires the
  `connector.Store` interface to expose a transaction method, which `internal/data`
  cannot satisfy structurally without naming `connector.Store` and closing an
  import cycle.
- **A repository interface per family, with mocks.** Rejected: nothing varies
  across that seam. One adapter is a hypothetical seam, and the tests that want a
  fake already have one at `connector.Store`, where two adapters really do exist.

## Consequences

- `Model.DB` is a `Handle` rather than a `*sql.DB`. Nothing outside the package
  constructed a model directly, so this is contained.
- The ten hand-rolled transactions become two, both inside the seam itself.
  `migrations.go` keeps its own, because it runs before any model exists.
- `connector.Store` loses `InsertSession`, `InsertSessionStats` and `InsertRoute`
  and gains `InsertWorkout`. The Apple Connector copies its route artifacts before
  the write, which it can: a content key does not depend on the Session's id.
- Two rules bind anything passed to `Tx`, both consequences of the single
  connection: do not hold a `*sql.Rows` open across another statement, and do not
  start a goroutine that touches the database. They are documented on `Tx`.
- `TestTxIsReentrant` would hang rather than fail if the nesting were ever broken.
  That is the honest shape of a deadlock, and it is why the rule is enforced by
  the type rather than by the test.
