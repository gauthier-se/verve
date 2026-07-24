// Metric ↔ chart-type mapping and derived-Metric presentation, mirroring the server.
import type { ChartType, Formula, Metric } from "./types";

/** defaultChartType mirrors the server's default chart for a Metric. */
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

/** compatibleChartTypes lists the chart types a Metric may switch among. */
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

/** formatFormula renders a Formula as a readable expression for a tooltip, e.g.
 *  "4·dietary_protein / dietary_energy × 100". */
export function formatFormula(formula: Formula, label: (slug: string) => string = (s) => s): string {
  let expr = weightedSum(formula.numerator, label);
  if (formula.denominator && formula.denominator.length > 0) {
    expr = `${expr} / ${weightedSum(formula.denominator, label)}`;
  }
  if (formula.scale !== 1) {
    expr = `${expr} × ${formula.scale}`;
  }
  return expr;
}

/** weightedSum joins Formula terms into "a·x + b·y − c·z", naming each operand via
 *  `label` (identity by default, or metricLabel for a human-readable tooltip). */
function weightedSum(terms: Formula["numerator"], label: (slug: string) => string): string {
  return terms
    .map((term, i) => {
      const coeff = Math.abs(term.coefficient);
      const name = label(term.metric);
      const factor = coeff === 1 ? name : `${coeff}·${name}`;
      if (i === 0) return term.coefficient < 0 ? `−${factor}` : factor;
      return term.coefficient < 0 ? ` − ${factor}` : ` + ${factor}`;
    })
    .join("");
}
