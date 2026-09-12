// Drawing one Night against the clock (ADR 0041): where each stage sits on the
// night's own span, and how a moment in it reads.
//
// The span is the night's, not the day's. A night is 23:14 to 06:41, and drawing
// it inside a midnight-to-midnight axis would cut it in two, which is the thing
// the Night was invented to stop (ADR 0027).
import type { NightInterval } from "./types";

/** NightBand is one interval placed on the night's span, in percentages so the
 *  chart is a handful of divs and needs no measuring pass. */
export interface NightBand {
  state: string;
  startAt: string;
  endAt: string;
  leftPct: number;
  widthPct: number;
}

/** nightSpan is the night's own bounds: the first interval's start and the last
 *  one's end. Null for a night with no intervals, which the page renders as the
 *  absence it is rather than as an empty axis. */
export function nightSpan(intervals: NightInterval[]): { from: number; to: number } | null {
  let from = Infinity;
  let to = -Infinity;
  for (const i of intervals) {
    const start = Date.parse(i.start_at);
    const end = Date.parse(i.end_at);
    if (Number.isNaN(start) || Number.isNaN(end)) continue;
    from = Math.min(from, start);
    to = Math.max(to, end);
  }
  if (from === Infinity || to <= from) return null;
  return { from, to };
}

/** nightBands places each interval on the span. Intervals are kept in the order
 *  the server sent them, which is start order, and a zero-length one is dropped:
 *  it would render as an invisible band and count as a stage the night had. */
export function nightBands(intervals: NightInterval[]): NightBand[] {
  const span = nightSpan(intervals);
  if (!span) return [];
  const total = span.to - span.from;

  const out: NightBand[] = [];
  for (const i of intervals) {
    const start = Date.parse(i.start_at);
    const end = Date.parse(i.end_at);
    if (Number.isNaN(start) || Number.isNaN(end) || end <= start) continue;
    out.push({
      state: i.state,
      startAt: i.start_at,
      endAt: i.end_at,
      leftPct: ((start - span.from) / total) * 100,
      widthPct: ((end - start) / total) * 100,
    });
  }
  return out;
}

/** hourTicks are the clock hours inside the span, as percentages, so the axis is
 *  labelled with times a person recognises rather than with elapsed minutes. The
 *  first tick is the first whole hour after the night began. */
export function hourTicks(intervals: NightInterval[]): { label: string; leftPct: number }[] {
  const span = nightSpan(intervals);
  if (!span) return [];
  const total = span.to - span.from;

  const out: { label: string; leftPct: number }[] = [];
  const first = new Date(span.from);
  first.setUTCMinutes(0, 0, 0);
  for (let t = first.getTime() + 3600_000; t < span.to; t += 3600_000) {
    out.push({ label: clockTime(new Date(t).toISOString()), leftPct: ((t - span.from) / total) * 100 });
  }
  return out;
}

/** clockTime renders a timestamp as the hour and minute of the night, in UTC.
 *  Verve stores and reads every bound in UTC, and a night rendered in one zone
 *  and labelled in another is a night that appears to have happened at a
 *  different time than its own figures say. */
export function clockTime(timestamp: string | undefined): string {
  if (!timestamp) return "";
  const t = Date.parse(timestamp);
  if (Number.isNaN(t)) return "";
  const d = new Date(t);
  return `${String(d.getUTCHours()).padStart(2, "0")}:${String(d.getUTCMinutes()).padStart(2, "0")}`;
}

/** efficiencyBasisLabel turns the payload's basis into words. The figure means
 *  something different depending on what it was divided by, so the page says
 *  which rather than printing a bare percentage (ADR 0041). */
export function efficiencyBasisLabel(basis: string | undefined): string {
  switch (basis) {
    case "onset_to_wake":
      return "from falling asleep to waking";
    case "time_in_bed":
      return "of time in bed";
    default:
      return "";
  }
}
