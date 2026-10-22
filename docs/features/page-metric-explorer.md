# Metric explorer

- **Status:** Planned
- **Milestone:** M2
- **Related:** [Storage and aggregates](data-storage-and-aggregates.md), [Deduplication and source priority](data-deduplication-and-source-priority.md), [Deletion and block rules](data-deletion-and-block-rules.md), [ADR 0003](../decisions/0003-tanstack-charts-behind-wrappers.md)

## Summary

A generic page that displays any stored metric over any period. It is the backbone of the dashboard: because every sample is stored generically, the explorer covers all metrics, including those without a dedicated page.

## User stories

- As a user, I want to look at any metric Apple Health recorded, even obscure ones.
- As a user, I want to compare this month to last month.
- As a user, I want to see which source each value comes from and fix bad data.
- As a user, I want to bookmark or share a link to a specific view.

## Scope

### In scope

- Metric picker, period and granularity controls, chart, statistics.
- Previous period comparison, moving average, secondary metric overlay.
- Source breakdown and raw samples table with delete and block actions.
- URL state.

### Out of scope

- Saving views as widgets (see [Custom dashboards](custom-dashboards.md)).

## Functional requirements

- **FR-1**: Metric picker with search, grouped by display group, listing only metrics with data, with first and last sample dates. Uncurated types appear under "Other".
- **FR-2**: Period presets: day, week, month, 6 months, year, all, custom range, with previous and next navigation.
- **FR-3**: Granularity: automatic by default (hour for a day, day for up to 3 months, week up to 2 years, month beyond), overridable.
- **FR-4**: Chart type follows the aggregation style: bars of sums for cumulative metrics, line of means with a min to max band for discrete metrics, timeline of intervals for category metrics.
- **FR-5**: Statistics panel: total (cumulative), mean, min, max, count of samples, number of days with data, and the change versus the previous period.
- **FR-6**: Options: overlay the previous period, show a 7-day moving average, overlay a second metric on a secondary axis (for example weight and calories consumed).
- **FR-7**: "Sources" tab: stacked breakdown per source over the period, the priority list for this metric with a link to edit it, and the combine mode toggle for discrete metrics.
- **FR-8**: "Samples" tab: raw samples for the selected period (limited to the raw read range) in a TanStack Table with time, value, unit, source, origin and metadata, sortable and paginated, with selection and delete or block actions.
- **FR-9**: The full view state (metric, period, granularity, options, tab) is stored in typed URL search params.
- **FR-10**: Tooltips show the value in the user's units, the bucket range and the source used.
- **FR-11**: A "Never store this metric" action creates a type block rule after confirmation.

## UX notes

- Mobile: metric picker as a full-screen sheet, period presets as a segmented control, statistics below the chart.
- Pinch and drag on the chart to zoom into a range, which updates the period.

## Data and API notes

- Uses the series and samples endpoints from [Storage and aggregates](data-storage-and-aggregates.md).
- Chart components: `TimeSeriesChart`, `RangeBandChart`, `IntervalTimeline`.

## Edge cases

- Metrics with very sparse data (a few samples per year): line chart with points, no interpolation over long gaps.
- Category metrics with many values (sleep stages, symptoms severity): color legend per value.
- Second metric with different units: separate axis, never mixed scales.

## Acceptance criteria

- [ ] Any metric present in storage can be displayed without code changes.
- [ ] Reloading a URL restores exactly the same view.
- [ ] Deleting a sample from the table updates the chart after recomputation.

## Future ideas

- Correlation view between two metrics (scatter plot with daily values).
- Annotations on charts (illness, vacation, injury).
