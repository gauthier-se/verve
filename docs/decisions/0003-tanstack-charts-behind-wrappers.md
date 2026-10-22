# 0003. TanStack Charts behind wrapper components

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Verve is chart-heavy: long time series, calendar heatmaps, hypnograms, synchronized cursors between workout charts and the map, plan trajectories. TanStack Charts reached 1.0 on 2026-10-03 (1.1.0 on 2026-10-09). It covers these needs with a grammar-of-graphics model, SVG rendering with optional Canvas per mark, synchronized cursors and a small bundle. Being very young, it has few examples and likely early bugs. Apache ECharts is the battle-tested alternative.

## Decision

- Use TanStack Charts.
- Pages and widgets never import the library directly. All charts go through Verve components in `web/src/components/charts/`, for example `TimeSeriesChart`, `RangeBandChart`, `CalendarHeatmap`, `Hypnogram`, `ZoneBars`, `TrajectoryChart`, `Sparkline`.
- Dense series (heart rate during a workout, multi-year daily series) use Canvas marks. Axes and interactions stay in SVG.

## Consequences

- Consistent, typed API across the stack and custom charts (hypnogram, trajectory) are natural to express.
- If the library blocks us, only the wrapper components need to be rewritten (for example on top of ECharts).
- The wrapper API must stay expressed in Verve concepts (series, buckets, ranges), not in library concepts.

## Alternatives considered

- **Apache ECharts**: proven and feature-rich, but heavier (about 157 kB gzip versus about 30 kB, figures from the TanStack Charts site), and custom charts need verbose `custom` series.
- **Recharts**: SVG only, struggles with dense series.
- **uPlot**: very fast but low-level, more custom code for heatmaps and hypnograms.
