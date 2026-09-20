// The Day page's pure layer: the date arithmetic its navigation walks on, and the
// one reading a figure row needs before it can be drawn (ADR 0043).
//
// A Day is a Bucket read as a page, not an entity, so there is nothing here that
// projects a shape or derives a grain. What a Day needs is smaller and duller than
// what a Night needs, which is the point.
import type { Day, DayMetric } from "./types";

/** today is the local wall-clock day. Local, because "today" is a thing about where
 *  the person is and not about UTC: at 01:00 in Paris the day the reader means is
 *  already the new one, and a UTC answer would open yesterday's page. */
export function today(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** shiftDay moves a YYYY-MM-DD date by whole days, crossing months, years and leap
 *  days by construction.
 *
 *  The arithmetic runs entirely in UTC and the result is read back as a UTC date, so
 *  no local offset can move it: `new Date("2026-03-08")` is midnight UTC, which in a
 *  western zone is still the 7th, and a page that shifted by a day on its way to the
 *  URL would be worse than no navigation at all. Same reason formatDay splits rather
 *  than parses. */
export function shiftDay(date: string, delta: number): string {
  const [y, m, d] = date.split("-").map(Number);
  if (!y || !m || !d) return date;
  return new Date(Date.UTC(y, m - 1, d + delta)).toISOString().slice(0, 10);
}

/** isValidDay reports whether a string is a real calendar date. The server answers
 *  422 for anything else, so this is what keeps a typed URL from asking. */
export function isValidDay(date: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false;
  return shiftDay(date, 0) === date;
}

/** RowState is what a figure row is saying, which is three different things and not
 *  two: a value, a gap (nothing was recorded), or a refusal (an Exclusion covers
 *  this Metric on this date, ADR 0033). Every other screen in Verve can only draw
 *  the first two, because only the Day holds the rule beside the absence. */
export type RowState = "value" | "gap" | "excluded";

export function rowState(row: DayMetric): RowState {
  if (row.value !== undefined) return "value";
  return row.excluded ? "excluded" : "gap";
}

/** isEmptyDay reports a date nothing happened on. It is a designed state and not an
 *  edge: previous/next navigation walks onto one the moment it passes the end of the
 *  Account's history, and a date exists whether or not anything happened on it. */
export function isEmptyDay(day: Day): boolean {
  return (
    day.metrics.length === 0 &&
    day.sessions.length === 0 &&
    day.annotations.length === 0 &&
    day.manual_entries.length === 0 &&
    day.night === undefined
  );
}
