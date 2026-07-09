import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { RangeTokens } from "@/lib/time-range";
import type { BaselineRule, Bucket, Series } from "@/lib/types";

/** BaselineParams is the Dashboard's comparison Baseline as a Panel consumes it:
 *  the rule plus, for `custom`, the absolute bounds. `none` means no comparison. */
export interface BaselineParams {
  rule: BaselineRule;
  from?: string | null;
  to?: string | null;
}

/** SeriesResult is one Panel's data: the current series and, in comparison mode,
 *  the equal-length index-aligned Baseline series the server overlays (ADR 0015). */
export interface SeriesResult {
  series: Series;
  baseline?: Series;
}

/** useSeries fetches one Panel's aggregated buckets (GET /v1/series). It forwards
 *  the Dashboard's range tokens, the comparison Baseline, and the Panel's optional
 *  bucket override; the server (timeaxis) resolves them into windows and a bucket.
 *  The query key spans every token, so a Panel refetches when the range, comparison
 *  rule, or its bucket changes — the mechanism behind "range and comparison update
 *  all Panels". */
export function useSeries(params: {
  metric: string;
  range: RangeTokens;
  bucket: Bucket | null;
  baseline?: BaselineParams;
}) {
  const { metric, range, bucket, baseline } = params;
  const comparing = baseline !== undefined && baseline.rule !== "none";

  return useQuery({
    queryKey: [
      "series",
      metric,
      range.preset,
      range.from,
      range.to,
      bucket,
      comparing ? baseline.rule : "none",
      comparing && baseline.from ? baseline.from : null,
      comparing && baseline.to ? baseline.to : null,
    ],
    queryFn: async (): Promise<SeriesResult> => {
      const qs = new URLSearchParams({ metric, range_preset: range.preset });
      // Only a custom range carries bounds; relative presets resolve server-side.
      if (range.preset === "custom") {
        if (range.from) qs.set("range_from", range.from);
        if (range.to) qs.set("range_to", range.to);
      }
      if (bucket) qs.set("bucket", bucket);
      if (comparing) {
        qs.set("baseline_rule", baseline.rule);
        if (baseline.rule === "custom") {
          if (baseline.from) qs.set("baseline_from", baseline.from);
          if (baseline.to) qs.set("baseline_to", baseline.to);
        }
      }
      return api<SeriesResult>(`/v1/series?${qs.toString()}`);
    },
  });
}
