# Manual entry — writable Measurements, deletable only when Manual

## Context

Until now the `measurements` table had exactly one writer: a Connector, running an
idempotent import (ADR 0006). That made the store append-only by construction — a
re-import adds only rows whose content key is new, and nothing ever needs to be
removed. The model was clean because no human ever touched it.

Two needs break that. First, a Metric can be missing or stale with no way to fix it:
on the reference Account `height` has **three** Measurements, the last from
2024-08-28, and an Account with no smart scale has no `body_mass` at all. Second,
grilling surfaced a case the import path cannot serve: a Source may be present but
*untrustworthy* — the reference Account's `body_fat_percentage` correlates with body
mass at **r = 0.9895** over 474 days, i.e. the scale reports a weight lookup, not a
composition measurement. The owner needs to enter a figure they trust and have it
win, without deleting what the device said.

The moment a human can write, the store stops being append-only, and "can I fix a
typo?" becomes a question the model has to answer.

## Decision

Introduce the **Manual entry**: a Measurement the Account types, carrying the
reserved Source `Manual`, deduplicated by the same content key as any imported row.

`Manual` does **not** compete in Source priority. Priority elects one winning Source
for the whole range (`ResolveSource` — "whole-range only, per-bucket resolution is
deferred", ADR 0003) and every read then filters `AND source = ?`. Ranking `Manual`
first under that scheme would make a single typed body mass the winner of the entire
window, collapsing a 905-point weight chart to one point. The mechanic priority
provides is the wrong shape for a source that is sparse by nature.

Instead a Manual entry **overlays** at day grain: on a day the Account has typed a
value for a Metric, that day's Manual rows replace the winning Source's rows; every
other day is untouched. The overlay is applied in the source predicate shared by
every read path, so bucketing, summaries and Formula operands each see one
already-resolved row set. It is skipped when the Metric has no Manual rows, which
keeps existing behaviour bit-identical for every Account that has never typed one.

Two endpoints: `POST /v1/measurements` (any Catalog slug, canonical unit only,
value + date with optional time) and `DELETE /v1/measurements/{id}`.

**Delete is refused with 403 when the Source is not `Manual`.** Correcting a manual
value means deleting it and re-entering.

## Considered Options

- **Manual as a real Source, delete restricted to it (chosen).** The Account's own
  measurements are as canonical as a device's; Source priority (ADR 0003) already
  exists to resolve exactly this kind of disagreement, and content keys (ADR 0006)
  already deduplicate. The feature costs one new Source string and one guarded
  delete.
- **Profile columns on `accounts` for height/mass/body fat.** Smallest change, no
  write to `measurements`. Rejected: it creates a *second* height — the column and
  the Metric — with nothing to say which is authoritative, and the two diverge
  silently. It also does nothing for an Account with no scale.
- **Delete any Measurement, whatever its Source.** Superficially more useful, and
  **silently broken**: the content key is what makes re-import idempotent, so
  deleting an imported row removes its key and the next Apple import reinserts the
  row verbatim. The deletion is undone with no warning. Making it stick needs a
  tombstone table consulted at import time — a feature in its own right, not a flag
  on this one.
- **Strictly append-only, correct by entering the right value on top.** Keeps the
  model immutable but leaves two same-day `Manual` rows in conflict, which Source
  priority *cannot* arbitrate (it ranks sources, not rows of the same source). It
  would need a new "latest write wins" rule, and `1840 cm` stays in the health
  record forever.

## Consequences

- The overlay is the one genuinely new read-path mechanic here, and it costs more
  than adding a row to the priority table — the existing resolution turned out to be
  the wrong shape, which was visible only from the code. Day grain, rather than the
  caller's bucket grain, keeps the resolved row set independent of how the caller
  happens to be bucketing, so a daily chart, a monthly chart and a window summary can
  never disagree about which rows are in play.
- Per-bucket Source resolution *between devices* stays deferred (ADR 0003). This ADR
  neither implements nor reuses it: devices produce continuous streams, where a
  whole-range winner is the right call; a human produces isolated corrections.
- `MeasurementModel` gains its first `Delete`. The guard belongs in the model's
  `WHERE` clause (`AND source = 'Manual'`), not only in the handler, so the
  invariant cannot be bypassed by a future caller.
- Measurement ids must now reach the client, which the Ledger's aggregated rows
  (ADR 0021) do not carry. Listing and deleting manual entries needs its own small
  read path scoped to `source = 'Manual'`.
- `Manual` becomes a reserved Source name. A Connector that reported a Source
  literally called "Manual" would collide; none does, and the Apple export names
  sources after devices and apps.
- This is a platform capability, not a feature of the Plan page: the Plan is simply
  its first consumer (ADR 0023).
