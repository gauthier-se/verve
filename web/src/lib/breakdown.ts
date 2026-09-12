// Drawing a Series that carries a breakdown: which segments it has, what they are
// called, and in what order they stack.
//
// Two Metrics are shaped this way and they are not the same question. Sleep sends
// minutes per Stage over a Night, a closed set with a fixed order and a fixed
// colour each (sleep.ts, ADR 0027). Training volume sends minutes or kilometres
// per Activity, an open set the server caps at five plus `other` (ADR 0040), so
// its order comes from the data and its labels from the Activity table the
// Catalog serves.
//
// What is common is the drawing, and that is what lives here. A component asks
// whether a Series has a breakdown, never whether it is sleep.
import { CATEGORY_COLORS } from "./chart";
import { stageColor, stageLabel, stagesPresent } from "./sleep";
import type { Activity, Point, Series } from "./types";

/** OTHER_SEGMENT is the key the server folds the capped-away Activities into. It
 *  is Verve's own bucket rather than a source's word, so it is labelled rather
 *  than prettified, and it always stacks last. */
export const OTHER_SEGMENT = "other";

/** hasBreakdown reports whether a Series carries per-segment values to stack.
 *  Components branch on this rather than on a Metric's name: a second by-state
 *  rule arrived once and will again. */
export function hasBreakdown(s: Series | undefined): boolean {
  return s?.aggregation === "duration_by_state" || s?.aggregation === "sum_by_state";
}

/** segmentsPresent lists the segments a set of Points actually contains, in stack
 *  order. A segment absent from the window is absent from the chart and from the
 *  table: an empty column explains nothing.
 *
 *  Sleep keeps its fixed Stage order, which is a statement about a night rather
 *  than about the data. An open set has no such order, so it takes the one the
 *  window's own totals give, largest first, with `other` last whatever its size:
 *  it is the only segment that is not a thing, so it does not compete for a place
 *  in the ranking. */
export function segmentsPresent(metric: string, points: Point[], summary?: Point | null): string[] {
  if (metric === "sleep") return stagesPresent(points);

  const totals = new Map<string, number>();
  const source = summary?.states ? [summary] : points;
  for (const p of source) {
    for (const [key, value] of Object.entries(p.states ?? {})) {
      totals.set(key, (totals.get(key) ?? 0) + value);
    }
  }

  return [...totals.keys()].sort((a, b) => {
    if (a === OTHER_SEGMENT) return 1;
    if (b === OTHER_SEGMENT) return -1;
    const diff = (totals.get(b) ?? 0) - (totals.get(a) ?? 0);
    return diff !== 0 ? diff : a.localeCompare(b);
  });
}

/** segmentLabel names one segment. Sleep labels a Stage; everything else labels an
 *  Activity from the Catalog's own table, which is where the curated names live
 *  (ADR 0002) — "HIIT" rather than four words. An Activity the table does not list
 *  is prettified from its slug rather than dropped, the same fallback the server
 *  applies to a slug it has never seen. */
export function segmentLabel(metric: string, key: string, activities?: Map<string, Activity>): string {
  if (metric === "sleep") return stageLabel(key);
  if (key === OTHER_SEGMENT) return "Other activities";
  return activities?.get(key)?.label ?? prettifySlug(key);
}

/** segmentColor is the colour one segment is drawn in. A Stage has a fixed slot,
 *  so a night with no REM does not repaint deep sleep. An Activity cannot: the set
 *  is open and the five on screen change with the window, so colour follows the
 *  position in the stack, and `other` always lands on the last slot so the bucket
 *  that is not a thing never wears a Metric's colour. */
export function segmentColor(metric: string, key: string, index: number): string {
  if (metric === "sleep") return stageColor(key, index);
  if (key === OTHER_SEGMENT) return CATEGORY_COLORS[CATEGORY_COLORS.length - 1];
  return CATEGORY_COLORS[index % CATEGORY_COLORS.length];
}

/** prettifySlug turns an unlisted slug into something readable, matching what the
 *  server does for an Activity its own table does not name. */
function prettifySlug(slug: string): string {
  return slug
    .split("_")
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

/** Segment is one stacked part of a bucket, resolved once: its key, the name it
 *  shows and the colour it is drawn in. The bars, the tooltip and the legend all
 *  read the same list, so a swatch and a label cannot disagree. */
export interface Segment {
  key: string;
  label: string;
  color: string;
}

/** buildSegments resolves a Series' breakdown into what a chart draws, or an
 *  empty list for a Series that has none. */
export function buildSegments(s: Series | undefined, activities?: Map<string, Activity>): Segment[] {
  if (!s || !hasBreakdown(s)) return [];
  return segmentsPresent(s.metric, s.points, s.summary).map((key, i) => ({
    key,
    label: segmentLabel(s.metric, key, activities),
    color: segmentColor(s.metric, key, i),
  }));
}
