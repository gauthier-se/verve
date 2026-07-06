// Time-range resolution (issue 06). A Dashboard's Time range is global to all
// its Panels; a preset or custom window resolves to concrete day-granularity
// bounds for GET /v1/series, and the bucket is auto-derived from the span unless
// a Panel overrides it.
import { differenceInCalendarDays, format, subDays, subMonths, subYears } from "date-fns";
import type { Bucket, Dashboard, Panel } from "./types";

/** ALL_FLOOR is the lower bound the "All" preset expands to — earlier than any
 *  plausible imported history, so it effectively means "everything". */
const ALL_FLOOR = "2000-01-01";

const DAY = "yyyy-MM-dd";

export interface ResolvedRange {
  from: string;
  to: string;
}

/** resolveRange turns a Dashboard's preset (or custom bounds) into a concrete
 *  [from, to] window in YYYY-MM-DD. `to` is today for every preset. */
export function resolveRange(d: Pick<Dashboard, "range_preset" | "range_from" | "range_to">): ResolvedRange {
  const today = new Date();
  const to = format(today, DAY);
  switch (d.range_preset) {
    case "7d":
      return { from: format(subDays(today, 7), DAY), to };
    case "30d":
      return { from: format(subDays(today, 30), DAY), to };
    case "3m":
      return { from: format(subMonths(today, 3), DAY), to };
    case "1y":
      return { from: format(subYears(today, 1), DAY), to };
    case "all":
      return { from: ALL_FLOOR, to };
    case "custom":
      // A custom range should always carry bounds; fall back to 30d if not.
      if (d.range_from && d.range_to) return { from: d.range_from, to: d.range_to };
      return { from: format(subDays(today, 30), DAY), to };
  }
}

/** autoBucket derives the bucket from a span: ≤31d→day, ≤1y→week, else month
 *  (issue 06), keeping the point count bounded without a per-Panel choice. */
export function autoBucket(range: ResolvedRange): Bucket {
  const days = Math.abs(differenceInCalendarDays(new Date(range.to), new Date(range.from)));
  if (days <= 31) return "day";
  if (days <= 366) return "week";
  return "month";
}

/** effectiveBucket is a Panel's own bucket override, or the auto-derived one. */
export function effectiveBucket(panel: Pick<Panel, "bucket">, range: ResolvedRange): Bucket {
  return panel.bucket ?? autoBucket(range);
}

/** The preset buttons shown on the Time-range control, in display order. */
export const RANGE_PRESETS: { value: Exclude<Dashboard["range_preset"], "custom">; label: string }[] = [
  { value: "7d", label: "7D" },
  { value: "30d", label: "30D" },
  { value: "3m", label: "3M" },
  { value: "1y", label: "1Y" },
  { value: "all", label: "All" },
];
