import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { BaselineRule, Bucket, Series } from "@/lib/types";

/** BaselineParams is the Dashboard's comparison Baseline as a Panel consumes it:
 *  the rule plus, for `custom`, the absolute bounds. `none` (or an absent value)
 *  means no comparison — the single-series request, unchanged. */
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

/** useSeries fetches one Panel's aggregated buckets (GET /v1/series). The query
 *  key includes every parameter — including the Baseline — so a Panel refetches
 *  when the Dashboard's range, comparison rule, or its own bucket changes; this is
 *  the mechanism behind "range and comparison update all Panels".
 *
 *  When a Baseline rule other than `none` is active, the request carries the
 *  `baseline` param and the response adds a baseline series the server has already
 *  aligned to the current one (the client never does baseline date math). */
export function useSeries(params: {
  metric: string;
  from: string;
  to: string;
  bucket: Bucket;
  baseline?: BaselineParams;
}) {
  const { metric, from, to, bucket, baseline } = params;
  const comparing = baseline !== undefined && baseline.rule !== "none";

  return useQuery({
    queryKey: [
      "series",
      metric,
      from,
      to,
      bucket,
      comparing ? baseline.rule : "none",
      comparing && baseline.from ? baseline.from : null,
      comparing && baseline.to ? baseline.to : null,
    ],
    queryFn: async (): Promise<SeriesResult> => {
      const qs = new URLSearchParams({ metric, from, to, bucket });
      if (comparing) {
        qs.set("baseline", baseline.rule);
        // Only the custom rule carries bounds; the relative rules are recomputed
        // server-side from the current range.
        if (baseline.rule === "custom") {
          if (baseline.from) qs.set("baseline_from", baseline.from);
          if (baseline.to) qs.set("baseline_to", baseline.to);
        }
      }
      return api<SeriesResult>(`/v1/series?${qs.toString()}`);
    },
  });
}
