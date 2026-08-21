# PRD — Design refresh: five screens, nine palettes

## Goal

Implement the `design_handoff_verve_screens` handoff: a refinement of the
Dashboard, Metric and Import screens, and two new ones — Cross-metric and
History. The handoff is a prototype written in HTML; the deliverable is the same
screens rebuilt in the repo's own stack (Vite + React 18, Tailwind, shadcn/ui,
TanStack Query/Router, Recharts, lucide-react), with every colour expressed as a
token.

The design's load-bearing decision is typographic, not chromatic: **every number,
date, bucket key and unit divisor is mono and tabular**. A dashboard is a grid of
figures read by comparison, and proportional digits make a column of them ragged.

## Constraints taken from the repo, not the handoff

- **Palette-agnostic or it is not done.** Nine palettes × light/dark (ADR 0024,
  ADR 0026). The handoff's colours are literals resolved from the dark Verve
  palette; each one goes back to being its token. Its ad-hoc greys become
  `muted-foreground` at an opacity, which tracks the palette instead of fighting
  it — a fixed mid-grey is a step down from white on a dark ground and a smear on
  a light one.
- **No web fonts.** The handoff proposes IBM Plex via Google Fonts. Verve makes
  no outbound requests by design (ADR 0028 makes the same choice for map tiles),
  so the default stacks are kept and the *scale* is implemented instead — which
  the handoff explicitly allows.
- **The repo's range presets are kept** (`7d 30d 3m 1y all`), not the handoff's
  (`1m 3m 6m 1y all`): they are persisted, validated server-side and enum-typed,
  and the design gains nothing from renaming them.
- **No date arithmetic in the client.** Anything the screens print about a
  window — resolved dates, the compared period, axis bounds — comes from the
  server (ADR 0012).

## What this milestone does

- **A typographic layer** (`components/ui/figure.tsx`): `Figure`, `Unit`,
  `Eyebrow`, `Meta`, `Chip`, `SectionTitle`, `ScreenTitle`, `Key`, `Dot`,
  `LegendItem`, `Track`. A `lib/chart.ts` naming every chart colour as a token.
  Tailwind gains the sub-12px scale the design uses (`3xs`, `2xs`, `heading`,
  `screen`) and its tracking values. Shadows are removed from in-page surfaces:
  elevation is `--card` against `--background`, plus a 1px border.
- **Dashboard**: sticky translucent header; a meta line naming the resolved
  window, the compared window and the grain; an auto-fit panel grid; panel cards
  rebuilt around a headline figure, a mono note saying how the figure was
  arrived at ("sum · weekly buckets", "stacked · 214 nights recorded"), a stage
  legend, and a sign legend for diverging bars.
- **Metric page**: the hero figure, a taller chart, server-resolved axis marks, an
  annotation chip strip, and four stat cards — Highest, Lowest, Readings,
  Coverage.
- **Cross-metric** (`/cross`, new): pairwise matrix, scatter of the strongest
  pair, ranked relationships. See ADR 0031.
- **History** (`/history`, new): the long band with phases and gaps, over the
  event ledger. See ADR 0032.
- **Import**: the three-step rail, the drop zone, the progress card, the report
  figure strip and the unmapped card.
- **Narrow screens**: below the sidebar breakpoint the sidebar is replaced by a
  slim bar of account controls and a bottom tab bar. The tab bar scrolls rather
  than hiding entries behind a "more" menu — six destinations is the whole app.

## Supporting API work

- `Point.count` — the rows behind each bucket. The design asks for a Readings
  column and a Readings stat; without this there is no honest number to put in
  them.
- `GET /v1/timeaxis` — resolved window, grain and compared window, so the
  interface can print the axis it draws on without computing a date.
- `GET /v1/covary` — ADR 0031.
- `GET /v1/history` — ADR 0032.

## What this milestone does NOT do

- **New palettes, or changes to the existing ones.** The design is expressed in
  the tokens that already exist.
- **A range preset on the History page.** Its subject is "all of it".
- **A p-value, or any significance claim, on Cross-metric.** See ADR 0031.
- **An unmapped-records browser.** The import report accounts for them in words;
  an inspector is its own feature.
- **Chart rendering changes.** Recharts stays; only colour sourcing, tick size
  and the recessive awake stage change.

## Issues

1. `01-api-reading-counts-and-timeaxis` — `Point.count`, `GET /v1/timeaxis`.
2. `02-api-covary` — rank correlation over the Pins, lag presets, ranking.
3. `03-api-history` — dense band, folded phases, event ledger.
4. `04-web-design-system` — the typographic layer, the token module, the scale.
5. `05-web-existing-screens` — shell, dashboard, metric page, ledger, import.
6. `06-web-new-screens` — `/cross` and `/history`.
