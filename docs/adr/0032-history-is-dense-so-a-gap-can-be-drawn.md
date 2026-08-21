# The History band is served dense, so that a gap can be drawn

## Context

Every read in Verve is sparse, and deliberately: a bucket with no data is absent
from the payload, because a gap is never a zero (ADR 0014). A chart draws through
it with `connectNulls` off, and the curve breaks where the data does.

That rule answers "how do I not lie about a missing week". It does not answer
"how do I *show* the missing week", and those are different questions. On a
Dashboard the second one barely comes up: a 3-month window with a hole in it is
read as a hole. Over eight years of history it is the main thing there is to see.
The stretches where nothing was recorded — a phone changed, a watch left in a
drawer, six months when none of this mattered — are facts about the history, and
they are invisible in a sparse series because a bucket that is not sent cannot be
shaded.

A client could find them: walk the points, and wherever two consecutive bucket
keys are further apart than one bucket, that is a gap. But "further apart than
one bucket" is bucket arithmetic, which is exactly what the client is not allowed
to do — `internal/timeaxis` owns boundaries, its Go and SQL sides are pinned to
each other by test, and a third implementation in a language where nothing
compares it to the other two is the thing ADR 0030 already forbade for
Annotations. The failure mode is the same and just as quiet: Recharts matches a
reference area's bounds against axis categories by equality, so a shaded span
computed from a date that disagrees with the server by one day renders nothing at
all, with no error anywhere.

## Decision

**The History band is a dense series**: one entry per bucket from the first
recorded one to the last, each empty one carrying `gap: true` and no value. The
runs of empty buckets are also returned pre-grouped, as `{from, to}` bucket keys,
so the client shades a span without scanning for one.

This is a **second shape, not a change to the first**. `GET /v1/series` is
untouched and stays sparse everywhere else; density is a property of this one
read, where the gaps are the subject.

**The window is the Account's own extent**, not a preset: from the first instant
any family recorded something to the last. **The grain follows that span** on the
same thresholds a Dashboard's auto-bucket uses, so eight years come out monthly
and a three-month-old Account comes out weekly — the right answer for it, rather
than a coarser one borrowed from a longer history.

**Phases arrive folded onto the same grid**, clamped to it, with an open Phase
running to the last drawn bucket. Same rule, same reason: a span the client
derived from dates would land between categories.

**The events are typed, not written.** The API sends a kind, a date, and the
figures behind it; the interface owns the words. Most of those words are Verve's
promises about the data — that a re-import adds only what is new, that a gap is
not filled with a zero, that nothing incoming is discarded — and a promise
belongs next to the evidence for it, in the language of the person reading it.

## Considered Options

- **A dense series for this read (chosen).** The server already owns the grid; it
  materialises it once, here.
- **Deriving gaps in the client from consecutive bucket keys.** A second
  implementation of bucket boundaries, whose disagreements with the first are
  invisible rather than loud. Rejected, for the same reason ADR 0030 rejected it.
- **Making every series dense.** It would make the gap rule uniform and it would
  inflate every Panel's payload with buckets that exist to say nothing — a year
  of daily body mass for someone who weighs in weekly is six times the rows for
  the same information. The sparse default is right; this read is the exception
  that earns density. Rejected.
- **A separate `/v1/gaps` endpoint.** Then the band's grain and the gap list have
  to agree, computed by two calls that can be made with different parameters.
  One reading, one call. Rejected.
- **Sending the events as finished sentences.** Simpler client, and it puts
  interface copy — the part most likely to be rewritten, and the part that would
  need translating first — behind a JSON API version. Rejected.
- **A range preset on this page.** Every other screen has one; this is the screen
  whose entire subject is "all of it". A preset here would answer a question the
  page does not ask. Rejected — the choice offered instead is *which Metric* the
  band draws.

## Consequences

- `GET /v1/history` answers the page in one call: span, band, gaps, Phases,
  events.
- An Account with no data gets a page rather than an error: no band, no events,
  and no window invented out of a preset.
- The band's first bucket can itself be a gap — the span is the union of every
  family, so a history that starts with a night of sleep opens on a month with no
  body mass in it. That is accurate, and it is the correct thing to show.
- The event ledger reads newest first, ties broken by kind (import, phase, note,
  source, origin), so a day carrying an import and the Phase it revealed reads in
  that order rather than at random.
- Import runs were already recorded (`imports`, since 0002) and were never read
  by anything. This is the read that makes that table worth having.
