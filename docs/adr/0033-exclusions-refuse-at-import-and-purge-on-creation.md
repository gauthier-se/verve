# Exclusions refuse at import and purge on creation

## Context

ADR 0022 gave the Account a write into `measurements` and, with it, a delete: a
Manual entry is removable because a human typo must be undoable. Every other
Measurement stayed permanent, and the ADR was explicit about why deleting an
imported row is not merely unsupported but broken:

> the content key is what makes re-import idempotent, so deleting an imported row
> removes its key and the next Apple import reinserts the row verbatim. The
> deletion is undone with no warning. Making it stick needs a tombstone table
> consulted at import time, a feature in its own right, not a flag on this one.

Two needs now sit on that sentence.

The first is the case ADR 0022 half-answered. On the reference Account
`body_fat_percentage` correlates with `body_mass` at r = 0.9895 over 474 days: the
scale reports a weight lookup, not a composition measurement. The Manual entry
lets the owner type a figure they trust on a given day. It does not let them say
the simpler and more accurate thing, which is that the device's series is not a
measurement of anything and should not be in a health record at all.

The second is that an import takes what it is given. An Apple export carries
everything Health ever recorded, and an Account may not want all of it: not this
Metric, or not this Metric before the year they started trusting the device.
There is no way to say so, and no way for a decision like that to survive until
the next export is dropped, six months later.

These read as two features, and building either alone builds the wrong half. A
delete with no import-time memory is a delete that quietly undoes itself. An
import filter with no purge leaves everything imported before the filter existed.
The tombstone table consulted at import and the remembered import filter are the
same table read at the same moment.

## Decision

Introduce the **Exclusion**: an Account-scoped, standing refusal naming a Catalog
Metric and, optionally, an inclusive span of days.

An Exclusion does exactly two things, and they are inseparable:

1. **No Connector may write what it names.** The set is loaded at the start of an
   import and every matching Record is dropped, counted, and reported.
2. **What was already stored under it is deleted when it is created**, in the same
   transaction that writes the rule.

The transaction is the decision. Each half alone fails invisibly: a rule with no
purge is a delete that did not happen, a purge with no rule is data that comes
back next month, and neither failure announces itself.

**An Exclusion is never a read-time predicate.** No query outside the import path
and the purge consults the table. Hiding rows instead of removing them would
leave the series, the Ledger, the History and the row counts each free to disagree
about what the Account holds, and would answer neither of the two needs: the data
would still be in the record, and the import would still spend its time on it.

**An Exclusion governs Connectors, never the Account.** `POST /v1/measurements` is
untouched, so a Manual entry on an excluded Metric is still accepted. Excluding
the scale's body fat and typing the DEXA figure is one gesture in two halves: what
a Connector writes is a stream Verve cannot argue with, what the owner types is
already the argument.

**The purge takes every Source, including `Manual`.** "Delete every body fat
value" means every one. The confirmation names the count first, and says that the
Manual rows among them are the part no re-import can restore.

**The set is the memory.** There is no saved import profile beside it. What the
next import will refuse is, by construction, the list the Account is looking at
when it starts that import.

**Deleting an Exclusion restores nothing.** It stops the refusal; the next import
brings back whatever the export still holds. That is the recovery path for
imported rows, and there is none for Manual ones.

> Amended by ADR 0039: an Archive taken before the purge is a copy, and the
> Connector that reads one back is the single Connector allowed to write the
> `Manual` Source, so a Manual row is no longer unrecoverable in principle. It
> is still refused while the Exclusion stands, and for the reason this ADR gives
> in the first place: a standing refusal is the more recent statement about that
> Metric than the day the value was typed.

Scope: **Measurements**. A slug that is derived (no rows of its own, ADR 0014) or
State-backed (`sleep`, ADR 0027) is refused with a 422 that says which it is.

## Considered Options

- **An Exclusion that refuses at import and purges on creation (chosen).** One
  object for both needs, one transaction, one predicate. It costs a table, a
  branch in the Record loop, and a count on the Report.
- **Read-time hiding: keep the rows, filter them out of every read.** Nothing is
  destroyed, so nothing needs a confirmation. Rejected: it is not what was asked
  for, and it puts the same predicate into `internal/query`'s every path, the
  Ledger, the History, the Session stats and the Formula operands, where one
  missed call site is a page that disagrees with the page beside it. It also
  leaves the import doing all of its work, which was half the request.
- **A delete endpoint with no rule, as ADR 0022 already refused.** Rejected there
  and rejected here, for the reason given there: the next import restores the rows
  verbatim, with no warning.
- **A separate saved import profile beside the exclusion set.** Two objects that
  must agree about the same thing, and a UI asking the owner to keep them in sync.
  The exclusion set already *is* what the next import will do.
- **`PATCH` on an Exclusion, to widen or narrow a span.** Rejected: widening has
  to purge again, so the edit path would be the delete path with a different verb,
  and there would be two statements in the codebase that remove Measurements
  in bulk. Editing is delete then create.
- **A Source axis, `(metric, source)`.** It reads as the sharper tool for the
  untrustworthy-scale case and is not: the scale is the only thing reporting body
  fat, so the Metric names it already. It also invites the per-Source questions
  Source priority answers at read time (ADR 0003). Deferrable in one direction
  only, so it stays deferred.
- **Route excluded Records into the Unmapped bin.** Symmetrical with ADR 0002's
  "nothing incoming is discarded", and exactly wrong: the bin would then keep, row
  for row, the data the owner asked to be rid of. The bin is for what the Catalog
  cannot read, not for what the Account refused.
- **Block Manual entries on an excluded Metric.** Superficially consistent, and it
  would remove the escape hatch that makes the feature worth having.

## Consequences

- `MeasurementModel` gains a bulk delete, and `Delete`'s comment, which states
  that no call site can remove imported data, stops being the whole truth. The new
  statement therefore lives beside it, with the qualification attached: an
  Exclusion may remove imported rows precisely because it also makes the removal
  stick.
- The purge and the count that previews it share one SQL predicate. A confirmation
  showing a number the delete would not match is an estimate, not a confirmation.
- The day is `date(start_at)` in UTC, the expression every day-grain read already
  buckets by, so a purge and a chart can never disagree about which day a row
  belongs to. Bounds are inclusive, like an Annotation's and unlike a resolved
  window's: both are days a person typed.
- The span predicate therefore exists twice: once in SQL, for the purge, and once
  in Go, for the import's refusal. They are pinned to each other by test, the same
  arrangement `internal/timeaxis` uses for bucket boundaries, because a day one
  side removes and the other re-admits is the feature lying in whichever
  direction. The pairing earns its keep immediately: a stored timestamp Apple's
  format could not parse is kept verbatim, `date()` returns NULL for it and the
  purge leaves the row standing, while a naive lexical comparison in Go would have
  refused it at the next import.
- `GET /v1/exclusions/preview` is the only count of raw Measurements the API
  serves. ADR 0012 refuses to serve the rows, not to say how many there are, and
  the number exists for a confirmation rather than for a chart.
- The History gains an `exclusion` event. It is dated to the day the decision was
  taken and carries the span it is about in its own fields, which is not the same
  thing: "excluded body fat from 2026" happens on one date and concerns another.
  A deleted Exclusion takes its event with it, since the History reads current
  state and is not an audit trail.
- An Account with no Exclusion pays nothing: an empty map, a lookup that misses,
  and a report identical to today's.
- Sleep and Sessions stay excludable only by refusal, not by silence. `sleep` is
  the slug that earns its 422: it is real, familiar and imported-looking, and its
  rows are in another table, so accepting it would write a rule that reports
  success and does nothing, forever.
