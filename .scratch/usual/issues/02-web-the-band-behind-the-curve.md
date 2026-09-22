Status: ready-for-human
Blocked by: 01

# 02: web: the band behind the curve

## What

In `panel-chart.tsx`, when a single-Metric Series carries `usual` on its points:

- an `Area` between `usual.low` and `usual.high`, drawn **first** so it sits behind
  every other mark, including the `band` chart type's min/max fill;
- a neutral fill, not a slot of the ramp: the Usual is not a Metric and must not
  borrow a Metric's grammar (ADR 0020, 0036, 0042). Low opacity, no stroke;
- `type="stepAfter"`: the Usual is one value per bucket, not a curve between them;
- `connectNulls={false}`: the band breaks where the Usual is nil, as the trend
  line does (ADR 0032).

Not drawn when the Panel has more than one Metric, or when comparison is on.

### Tooltip

Below the value: "usual 46 to 52 (28 days before)" or "(12 weeks before)", with N
when it is under the full window ("usual 46 to 52, from 17 days"). Direction may be
stated as "above", "within" or "below". No colour, no icon.

### Legend

One line naming the band, so a reader does not take it for the min/max spread:
"Band: your p25 to p75 over the preceding 28 days".

## Checks

- `band` chart type (resting heart rate): Usual behind, min/max on top, both
  legible in light and dark, every palette;
- `bar` (steps): band behind the bars, today's bar without one;
- a history with a five-week gap: the band breaks and comes back after 14 days;
- month grain: nothing drawn, no legend line;
- phone width.

## Comments

Built test-first at two seams: `mergeSeries` (`usual0` for the band, `usual` for
the tooltip; none in comparison or on a combo) and `usualLine` in `lib/usual.ts`
(position, range, and the basis: "28 days before", or "from 17 of the 28 days
before"). The band is an `Area` drawn first, in the Baseline's recessed tone.

Changed from the draft above:

- **A centred step (`type="step"`), not `stepAfter`**: on a category axis
  `stepAfter` runs from one point to the next and sits half a bucket off.
- **No legend line**: Panels have no legend region (the trend has none either),
  so the tooltip names the band.

Left for a human: the visual checks listed above, in a signed-in browser (light
and dark, every palette, phone width). The reference data ends on 24 July 2026,
so use a custom range.
