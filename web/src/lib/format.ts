// Panel-summary number formatting (ADR 0019). FR locale — comma decimal, space
// thousands — compact-but-honest: a large sum is abbreviated for glanceability, while
// smaller and non-sum values keep full precision. The exact value lives in a tooltip.
import type { Aggregation, Bucket } from "./types";

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

/** formatDuration renders a figure in minutes as hours and minutes: 432 → "7h 12m",
 *  47 → "47m". Sleep's canonical unit is the minute, and a night reported as "432"
 *  is a number nobody reads as a duration. Keyed off the unit rather than the Metric,
 *  so any minute-valued figure can use it. */
export function formatDuration(minutes: number): string {
  const total = Math.round(Math.abs(minutes));
  const sign = minutes < 0 ? "-" : "";
  const h = Math.floor(total / 60);
  const m = total % 60;
  return h === 0 ? `${sign}${m}m` : `${sign}${h}h ${m}m`;
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

/** formatPace renders a speed in km/h as minutes per kilometre: 10 → "6:00/km".
 *  A runner reads a pace and a cyclist reads a speed, so which of the two a
 *  workout shows comes from its Activity's `reading` (ADR 0028) and never from a
 *  guess in the component. formatDuration cannot produce this: it rounds to the
 *  minute, and a pace is read to the second. */
export function formatPace(kmh: number): string {
  if (!Number.isFinite(kmh) || kmh <= 0) return "—";
  const secondsPerKm = Math.round(3600 / kmh);
  const m = Math.floor(secondsPerKm / 60);
  const s = secondsPerKm % 60;
  return `${m}:${String(s).padStart(2, "0")}/km`;
}

/** formatSpeed renders a speed in km/h: 28.4 → "28,4 km/h". */
export function formatSpeed(kmh: number): string {
  if (!Number.isFinite(kmh)) return "—";
  return `${nf({ maximumFractionDigits: 1 }).format(kmh)} km/h`;
}

/** formatSessionDuration renders a workout's duration, given in seconds, as
 *  "1h 04m" or "48m". Seconds rather than minutes because that is the unit a
 *  Session stores, and rounding to minutes first loses a short interval entirely. */
export function formatSessionDuration(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  if (h === 0) {
    const s = total % 60;
    return m === 0 ? `${s}s` : `${m}m`;
  }
  return `${h}h ${String(m).padStart(2, "0")}m`;
}

/** formatDay renders a server-sent YYYY-MM-DD as "20 Aug 2026". The input is
 *  already a plain day the server resolved, so it is split rather than parsed into a
 *  Date: `new Date("2026-08-20")` is midnight UTC, which in a western timezone
 *  renders as the 19th. A date the server named must not shift by a day on its way
 *  to the screen. */
export function formatDay(day: string, opts: { year?: boolean } = {}): string {
  const [y, m, d] = day.split("-").map(Number);
  if (!y || !m || !d) return day;
  const month = MONTHS[m - 1] ?? String(m);
  return opts.year === false ? `${d} ${month}` : `${d} ${month} ${y}`;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** formatDayRange renders a resolved window: "20 Aug 2025 → 20 Aug 2026", dropping
 *  the first year when both ends share it. */
export function formatDayRange(from: string, to: string): string {
  const sameYear = from.slice(0, 4) === to.slice(0, 4);
  return `${formatDay(from, { year: !sameYear })} → ${formatDay(to)}`;
}

/** formatBucketKey renders a bucket's start date as the key that bucket *is*:
 *  "2026-08-20" for a day, "2026-W33" for a week, "2026-08" for a month.
 *
 *  It is the table's counterpart to the chart's human label. A column of "Mar 4",
 *  "Mar 11", "Mar 18" reads as three dates; a column of "2026-W10", "2026-W11",
 *  "2026-W12" reads as a sequence, sorts as one, and can be pasted into a
 *  spreadsheet as one. Which is what a Ledger is for (ADR 0021).
 *
 *  The week number is derived, not stored — but only from a date the server already
 *  named as an ISO week start, and by the ISO rule the server buckets with (the week
 *  containing the Thursday). It labels a boundary; it never decides one. */
export function formatBucketKey(day: string, bucket: Bucket): string {
  if (bucket === "day") return day;
  const [y, m, d] = day.split("-").map(Number);
  if (!y || !m || !d) return day;
  if (bucket === "month") return `${y}-${String(m).padStart(2, "0")}`;

  // The Thursday of this week decides which year the week belongs to, which is the
  // whole of the ISO rule: a week is in the year that holds most of it.
  const thursday = new Date(Date.UTC(y, m - 1, d + 3));
  const isoYear = thursday.getUTCFullYear();
  const jan1 = Date.UTC(isoYear, 0, 1);
  const week = Math.floor((thursday.getTime() - jan1) / (7 * 24 * 3600 * 1000)) + 1;
  return `${isoYear}-W${String(week).padStart(2, "0")}`;
}
