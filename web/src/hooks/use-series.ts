import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { seriesParams } from "@/lib/series-url";
import type { RangeTokens } from "@/lib/time-range";
import type { BaselineRule, Bucket, Series } from "@/lib/types";

/** BaselineParams is the Dashboard's comparison Baseline as a Panel consumes it:
 *  the rule plus, for `custom`, the absolute bounds. `none` means no comparison. */
export interface BaselineParams {
  rule: BaselineRule;
  from?: string | null;
  to?: string | null;
}

/** SeriesResult is one Panel's data: one Series per Panel Metric in Panel order,
 *  and — single-Metric comparison mode only — the equal-length index-aligned
 *  Baseline series the server overlays (ADR 0015). On a multi-Metric Panel the
 *  server cuts the Baseline (ADR 0020), so it is never present here. */
export interface SeriesResult {
  series: Series[];
  baseline?: Series;
}

/** useSeries fetches one Panel's aggregated buckets (GET /v1/series), one
 *  repeated `metric` param per Panel Metric so the server resolves the time axis
 *  once for all of them (ADR 0020). It forwards the Dashboard's range tokens,
 *  the comparison Baseline, and the Panel's optional bucket override; the query
 *  key spans every token, so a Panel refetches when the range, comparison rule,
 *  or its bucket changes — the mechanism behind "range and comparison update
 *  all Panels". */
export function useSeries(params: {
  metrics: string[];
  range: RangeTokens;
  bucket: Bucket | null;
  baseline?: BaselineParams;
}) {
  const { metrics, range, bucket, baseline } = params;
  const comparing = baseline !== undefined && baseline.rule !== "none";

  return useQuery({
    queryKey: [
      "series",
      metrics.join(","),
      range.preset,
      range.from,
      range.to,
      bucket,
      comparing ? baseline.rule : "none",
      comparing && baseline.from ? baseline.from : null,
      comparing && baseline.to ? baseline.to : null,
    ],
    queryFn: async (): Promise<SeriesResult> => {
      // The same builder the CSV download uses, so the file and the curve ask
      // the same question (lib/series-url.ts).
      const qs = seriesParams({ metrics, range, bucket, baseline: comparing ? baseline : undefined });
      // One metric keeps the scalar envelope; several come back as an array
      // (ADR 0020). Normalize to a list either way.
      const raw = await api<{ series: Series | Series[]; baseline?: Series }>(
        `/v1/series?${qs.toString()}`,
      );
      return Array.isArray(raw.series)
        ? { series: raw.series }
        : { series: [raw.series], baseline: raw.baseline };
    },
  });
}
