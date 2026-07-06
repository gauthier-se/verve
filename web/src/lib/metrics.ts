// Metric ↔ chart-type mapping (issue 06). The default chart type is derived from
// a Metric's aggregation rule; the user may switch among the compatible types.
import type { Aggregation, ChartType } from "./types";

/** defaultChartType mirrors the server: sum→bar, average→band, latest→line,
 *  duration_by_state→stacked bar. */
export function defaultChartType(agg: Aggregation): ChartType {
  switch (agg) {
    case "sum":
      return "bar";
    case "average":
      return "band";
    case "duration_by_state":
      return "stacked_bar";
    case "latest":
      return "line";
  }
}

/** compatibleChartTypes lists the chart types a Metric may switch among, given
 *  its aggregation. The band variant is average-only and stacked bar sleep-only;
 *  bar/line/area suit any scalar Metric. */
export function compatibleChartTypes(agg: Aggregation): ChartType[] {
  switch (agg) {
    case "sum":
      return ["bar", "line", "area"];
    case "average":
      return ["band", "line", "area", "bar"];
    case "latest":
      return ["line", "area", "bar"];
    case "duration_by_state":
      return ["stacked_bar"];
  }
}

export const CHART_TYPE_LABEL: Record<ChartType, string> = {
  bar: "Bar",
  line: "Line",
  area: "Area",
  band: "Line + band",
  stacked_bar: "Stacked bar",
};

/** metricLabel humanizes a Catalog slug for display: heart_rate → "Heart rate". */
export function metricLabel(slug: string): string {
  const spaced = slug.replace(/_/g, " ");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}
