Status: ready-for-agent
Blocked by: 01

# 03: web: the shape of one night

## What

- **A page at `/nights/$date`** (`web/src/components/night-page.tsx`,
  registered in `router.tsx` beside `/workouts/$sessionId`, which it is modelled
  on): one entity, addressed by its own identity, with its figures above and its
  shape below.
- **The hypnogram is stage bands against the clock**, from onset to wake, one
  row per Stage in the existing stack order so a night reads top to bottom the
  way the stacked bar reads bottom to top. Colours are the Stage slots that
  already exist (`lib/sleep.ts`), unchanged, so a night's deep sleep is the same
  colour on both screens. `awake` keeps its recessed tone: it is stacked so a
  broken night looks broken, and it is not a fourth kind of sleep (ADR 0027).
- **The X axis is clock time**, labelled at the hour, spanning the night's own
  bounds and not a day: a night is 23:14 to 06:41 and drawing it inside a
  midnight-to-midnight axis would put it in two pieces, which is the thing the
  Night was invented to stop.
- **The figures above it**: asleep, awake, onset and wake as clock times,
  efficiency with its basis named beside it, and latency when the night has one.
  An absent figure is absent, never a dash pretending to be zero, and the
  efficiency basis is rendered as words ("of time in bed", "from falling asleep
  to waking") rather than as the payload's slug.
- **Reached from the sleep Metric page**: each bar and each table row is a Night
  already keyed by its label, so both become links. The Data page's sleep row
  points at the Metric page as it does today; nothing else changes.
- **`lib/night.ts`** holds what is pure and therefore testable: turning the
  payload's intervals into the bands a chart draws (clamped to the night's
  bounds, ordered, zero-length dropped), and formatting a clock time from a
  timestamp. Its own `night.test.ts`, since that is the layer the runner covers.
- **The empty and error states** follow the Session page's: a 404 is "no night
  was recorded on that morning", which is a fact about the data rather than an
  error, and the page says it in the app's own register.

## Why here

A page rather than a drawer or an expanded row, because the night is an entity
now and the app already has a shape for one: `/workouts/$sessionId` is the
precedent, down to the header, the figures and the chart under them. A drawer
would make the night unlinkable, and the first thing anyone wants to do with a
bad night is send it to somebody.

The Stage colours are reused rather than chosen, and that is the point: two
screens showing the same night in different colours is how a reader learns that
one of them is decorative.
