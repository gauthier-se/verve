# A standalone chart is coloured by its Metric

Extends ADR 0036, and narrows one sentence of it. Everything ADR 0020 and ADR 0036
decided about **Panels** stays in force: a Panel holds at most four Metrics, its
colours are assigned by position and never by Metric identity, and a Dashboard
rotates the ramp by each Panel's place in the grid. What changes is the clause ADR
0036 added almost in passing — "a chart that stands alone on its own page takes 0".

## Context

ADR 0036 gave a Panel an offset into the ramp because a Dashboard of single-Metric
Panels was drawing every card in `chart-1`, leaving three quarters of the ramp as
furniture nobody saw. It fixed the grid and left the same defect standing one screen
away: a Metric page is a single chart at position 0 with no offset, so **every detail
page in the application is `chart-1`**, and so is its annotation strip. Walking the
Catalog is a tour of one colour — the exact complaint ADR 0036 was written to answer,
in the one place its fix does not reach.

ADR 0036 considered and rejected "colour by Metric identity, hashed from the slug".
That rejection was correct *for a Dashboard* and its reasons are Dashboard reasons:
a Metric that kept its hue when it moved would let a reader believe two cards are
about one thing, and a hash over four slots distributes badly across a grid where the
collisions are visible side by side.

Neither reason survives the move to a standalone page. There is no slot to own, no
second Series to be confused with, no reorder to survive, and never two Metrics on
screen at once for a collision to be visible in.

## Decision

**A chart that stands alone on its own page reads the ramp from an offset derived
from its Metric's slug**, via `standaloneColorOffset` in `web/src/lib/chart.ts`
(FNV-1a, modulo the ramp). A Panel's offset is unchanged and still comes from the
grid.

**The rule is stated by scope, not by mechanism.** Colour belongs to the slot
wherever Metrics are compared — within a Panel, and across the Panels of a
Dashboard. Where nothing is compared, there is no slot, and the Metric is the only
stable thing to hang a colour on.

**The offset is uniform, which is the invariant that actually matters.** It shifts
the whole ramp rather than picking a colour per Series, so it cannot fold two Series
onto one colour and the four-way separation ADR 0026 verifies holds at every offset —
the same property that made ADR 0036's rotation safe.

**FNV-1a rather than a sum of character codes**, because the Catalog's slugs share
long prefixes (`dietary_`, `walking_`, `running_`) and a sum maps a whole family onto
neighbouring slots. Over the Catalog's 106 Metrics the hash spreads 36/21/28/21,
which is uneven and entirely adequate for a surface that shows one Metric at a time.

## Considered Options

- **Colour a detail page by the Metric's slug (chosen).** Stable, needs no state, and
  the page recolours only when the Metric changes — which is the one thing the reader
  is navigating by.
- **Leave it at 0, as ADR 0036 said.** Defensible on the letter of the rule and it is
  what shipped. Rejected on what it looks like: the rule's *purpose* is to use the
  ramp, and here it spends the ramp on nothing.
- **Offset by the Metric's position in the Catalog.** Also identity-derived, and worse:
  inserting a Metric in the Catalog would recolour every page after it, so the value
  is stable only until the Catalog grows.
- **Offset by the Metric's row position in the Data page it was reached from.** Keeps
  the letter of "colour belongs to the slot" and makes a page's colour depend on how
  you arrived at it. Two routes to one Metric would give two colours.
- **Give a Metric a colour in the Catalog.** The honest version of identity colouring,
  and the one ADR 0020 refused: it would leak back onto Panels, where it is wrong, and
  it is Catalog data serving a purely visual purpose.

## Consequences

- A Metric page, its chart and its annotation strip share one colour, and different
  Metrics differ. Nothing else on the page changes.
- The rule now has two halves and both must be said when it is explained: by slot on a
  Panel, by Metric on a page of its own. A single sentence no longer covers it, which
  is the cost of the fix.
- Other standalone surfaces — the Night page, a Session detail, the cross-metric
  matrix — are untouched here. They draw categories rather than a Metric's Series, and
  they read `CATEGORY_COLORS` by position, which is its own rule and stays.
- `seriesColor` is unchanged, so every Panel and Dashboard renders exactly as before.
  The new helper is a second entry point, not a change to the old one.
