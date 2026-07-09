// The Dashboard's Time range is resolved server-side (internal/timeaxis): the SPA
// forwards the stored range tokens to GET /v1/series and never computes dates or
// buckets. This module holds the preset list and the token projection.
import type { Dashboard } from "./types";

/** RangeTokens is the Dashboard's Time-range choice as the API consumes it. */
export interface RangeTokens {
  preset: Dashboard["range_preset"];
  from: string | null;
  to: string | null;
}

export function rangeTokens(d: Pick<Dashboard, "range_preset" | "range_from" | "range_to">): RangeTokens {
  return { preset: d.range_preset, from: d.range_from, to: d.range_to };
}

/** The preset buttons shown on the Time-range control, in display order. */
export const RANGE_PRESETS: { value: Exclude<Dashboard["range_preset"], "custom">; label: string }[] = [
  { value: "7d", label: "7D" },
  { value: "30d", label: "30D" },
  { value: "3m", label: "3M" },
  { value: "1y", label: "1Y" },
  { value: "all", label: "All" },
];
