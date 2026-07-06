import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Bucket, Series } from "@/lib/types";

/** useSeries fetches one Panel's aggregated buckets (GET /v1/series). The query
 *  key includes every parameter, so a Panel refetches when the Dashboard's range
 *  or its own bucket changes — the mechanism behind "range updates all Panels". */
export function useSeries(params: { metric: string; from: string; to: string; bucket: Bucket }) {
  const { metric, from, to, bucket } = params;
  return useQuery({
    queryKey: ["series", metric, from, to, bucket],
    queryFn: async () => {
      const qs = new URLSearchParams({ metric, from, to, bucket });
      const { series } = await api<{ series: Series }>(`/v1/series?${qs.toString()}`);
      return series;
    },
  });
}
