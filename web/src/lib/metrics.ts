// Metric ↔ chart-type mapping (issue 06) plus derived-Metric presentation (issue
// 04). The default chart type mirrors the server: a signed derived Metric defaults
// to a diverging bar (ADR 0014), otherwise the aggregation rule decides. The user
// may switch among the compatible types.
import type { ChartType, Formula, Metric } from "./types";

/** defaultChartType mirrors the server: a signed derived Metric → diverging bar;
 *  otherwise sum→bar, average→band, latest→line, duration_by_state→stacked bar,
 *  and an unsigned derived Metric (no rule) → line. */
export function defaultChartType(metric: Metric): ChartType {
  if (metric.signed) return "diverging_bar";
  switch (metric.aggregation) {
    case "sum":
      return "bar";
    case "average":
      return "band";
    case "duration_by_state":
      return "stacked_bar";
    default: // latest, and unsigned derived Metrics (no aggregation rule)
      return "line";
  }
}

/** compatibleChartTypes lists the chart types a Metric may switch among. The band
 *  variant is average-only and stacked bar sleep-only; the diverging bar is
 *  signed-only (it needs a sign to color); bar/line/area suit any scalar Metric. */
export function compatibleChartTypes(metric: Metric): ChartType[] {
  if (metric.signed) return ["diverging_bar", "bar", "line", "area"];
  switch (metric.aggregation) {
    case "sum":
      return ["bar", "line", "area"];
    case "average":
      return ["band", "line", "area", "bar"];
    case "duration_by_state":
      return ["stacked_bar"];
    default: // latest, and unsigned derived Metrics
      return ["line", "area", "bar"];
  }
}

export const CHART_TYPE_LABEL: Record<ChartType, string> = {
  bar: "Bar",
  line: "Line",
  area: "Area",
  band: "Line + band",
  stacked_bar: "Stacked bar",
  diverging_bar: "Diverging bar",
};

/** metricLabel humanizes a Catalog slug for display: heart_rate → "Heart rate". */
export function metricLabel(slug: string): string {
  const spaced = slug.replace(/_/g, " ");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/** formatFormula renders a derived Metric's Formula as a readable expression for a
 *  tooltip, e.g. "dietary_energy − active_energy − basal_energy" or
 *  "4·dietary_protein / dietary_energy × 100". The denominator is omitted when it
 *  is the implicit 1, and a ×scale suffix only shown when the scale isn't 1. */
export function formatFormula(formula: Formula): string {
  let expr = weightedSum(formula.numerator);
  if (formula.denominator && formula.denominator.length > 0) {
    expr = `${expr} / ${weightedSum(formula.denominator)}`;
  }
  if (formula.scale !== 1) {
    expr = `${expr} × ${formula.scale}`;
  }
  return expr;
}

/** weightedSum joins Formula terms into "a·x + b·y − c·z", folding each sign into
 *  the connecting operator and dropping a unit (±1) coefficient. */
function weightedSum(terms: Formula["numerator"]): string {
  return terms
    .map((term, i) => {
      const coeff = Math.abs(term.coefficient);
      const factor = coeff === 1 ? term.metric : `${coeff}·${term.metric}`;
      if (i === 0) return term.coefficient < 0 ? `−${factor}` : factor;
      return term.coefficient < 0 ? ` − ${factor}` : ` + ${factor}`;
    })
    .join("");
}
