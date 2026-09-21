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

/** Percent Metrics are stored as fractions — `body_fat_percentage` is 0.27, not 27, and
 *  `oxygen_saturation` is 0.969. Nobody will type 0.27, so a field is presented in 0–100
 *  and converted with these. Keyed off the Catalog unit, in exactly one place: a second
 *  copy of this rule is how a 26-point error that still looks plausible gets shipped.
 *  Shared by the Manual entry and the Goal form, which both take a typed value.
 *
 *  toDisplayValue is also every screen's rule, through the figure formats in format.ts:
 *  a stored 0.969 printed raw rounds to "1 %". The scaling is trimmed to 12 significant
 *  digits, because 0.969 × 100 is 96.89999999999999 in binary and a copied cell or an
 *  input's default would show it. */
const trim = (v: number) => Number(v.toPrecision(12));
export const isPercentUnit = (unit: string) => unit === "%";
export const toStoredValue = (unit: string, typed: number) => (isPercentUnit(unit) ? trim(typed / 100) : typed);
export const toDisplayValue = (unit: string, stored: number) => (isPercentUnit(unit) ? trim(stored * 100) : stored);
