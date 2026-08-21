import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { RangeTokens } from "@/lib/time-range";
import type { TimeAxis } from "@/lib/types";
import type { BaselineParams } from "./use-series";

/** useTimeAxis resolves a set of range tokens into the window, bucket and compared
 *  window they stand for (GET /v1/timeaxis).
 *
 *  It exists so the interface can *name* the axis it is drawing on — "20 Aug 2025 →
 *  20 Aug 2026", "against the previous 12 months", "bucket week" — without computing
 *  a single date. The browser's clock and zone are not the server's, and a label
 *  derived here would disagree with the buckets beside it near midnight and twice a
 *  year (ADR 0012). The query key mirrors useSeries', so the axis and the Series on
 *  it refetch together. */
export function useTimeAxis(params: { range: RangeTokens; baseline?: BaselineParams; bucket?: string | null }) {
  const { range, baseline, bucket } = params;
  const comparing = baseline !== undefined && baseline.rule !== "none";

  return useQuery({
    queryKey: [
      "time-axis",
      range.preset,
      range.from,
      range.to,
      bucket ?? null,
      comparing ? baseline.rule : "none",
      comparing && baseline.from ? baseline.from : null,
      comparing && baseline.to ? baseline.to : null,
    ],
    queryFn: async (): Promise<TimeAxis> => {
      const qs = new URLSearchParams({ range_preset: range.preset });
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
      const raw = await api<{ time_axis: TimeAxis }>(`/v1/timeaxis?${qs.toString()}`);
      return raw.time_axis;
    },
    // The resolved window only moves when the tokens do, or at midnight UTC. It is
    // cheap, but it is not worth refetching on every remount of a header.
    staleTime: 5 * 60 * 1000,
  });
}
