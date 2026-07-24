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
 *  meaningless) or when the Baseline is zero (no percentage base). The arrow follows
 *  the *shown* magnitude, so a change that rounds to zero reads as neutral ("→ 0 %")
 *  rather than a misleading "↑ 0 %". */
export function computeDelta(
  current: number,
  baseline: number,
  aggregation: Aggregation | "",
  signed: boolean,
): Delta {
  const diff = current - baseline;
  const usePercent = !signed && baseline !== 0;

  let label: string;
  let shownZero: boolean;
  if (usePercent) {
    const rounded = Math.round(Math.abs((diff / baseline) * 100));
    label = `${nf({ maximumFractionDigits: 0 }).format(rounded)} %`;
    shownZero = rounded === 0;
  } else {
    const magnitude = Math.abs(diff);
    label = formatSummaryValue(magnitude, aggregation);
    shownZero = magnitude === 0 || label === "0";
  }

  const arrow = shownZero ? "→" : diff > 0 ? "↑" : "↓";
  return { arrow, label, exact: formatExact(Math.abs(diff)) };
}
