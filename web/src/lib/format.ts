// Panel-summary number formatting (ADR 0019). FR locale — comma decimal, space
// thousands — compact-but-honest: a large sum is abbreviated for glanceability, while
// smaller and non-sum values keep full precision. The exact value lives in a tooltip.
import type { Aggregation } from "./types";

const LOCALE = "fr-FR";
const nf = (opts: Intl.NumberFormatOptions) => new Intl.NumberFormat(LOCALE, opts);

// A sum only abbreviates once it is genuinely large; below this it reads fine in full.
const COMPACT_FROM = 10_000;

/** formatSummaryValue renders a summary figure: a large `sum` abbreviates ("245 k"),
 *  everything else keeps full precision ("58", "74,2"). */
export function formatSummaryValue(value: number, aggregation: Aggregation | ""): string {
  if (aggregation === "sum" && Math.abs(value) >= COMPACT_FROM) {
    return nf({ notation: "compact", maximumFractionDigits: 1 }).format(value);
  }
  return nf({ maximumFractionDigits: 1 }).format(value);
}

/** formatExact is the full grouped value for a tooltip: "245 321", "74,2". */
export function formatExact(value: number): string {
  return nf({ maximumFractionDigits: 2 }).format(value);
}

/** Delta is a summary's headline change against its Baseline: a direction plus a
 *  magnitude, never a good/bad color (ADR 0015, ADR 0019). */
export interface Delta {
  arrow: "↑" | "↓" | "→";
  label: string; // the shown magnitude, e.g. "12 %" or "26 k"
  exact: string; // the absolute difference, exact, for the tooltip
}

/** computeDelta compares a summary to its Baseline summary: a percentage by default,
 *  but the absolute difference for a signed Metric (a percentage around zero is
 *  meaningless) or when the Baseline is zero (no percentage base). */
export function computeDelta(
  current: number,
  baseline: number,
  aggregation: Aggregation | "",
  signed: boolean,
): Delta {
  const diff = current - baseline;
  const arrow = diff > 0 ? "↑" : diff < 0 ? "↓" : "→";
  const usePercent = !signed && baseline !== 0;
  const label = usePercent
    ? `${nf({ maximumFractionDigits: 0 }).format(Math.abs((diff / baseline) * 100))} %`
    : formatSummaryValue(Math.abs(diff), aggregation);
  return { arrow, label, exact: formatExact(Math.abs(diff)) };
}
