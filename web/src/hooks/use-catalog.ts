import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Metric } from "@/lib/types";

/** useMetrics loads the Catalog (GET /v1/metrics). It rarely changes, so it is
 *  cached for the session and reused by the panel builder and every chart. */
export function useMetrics() {
  return useQuery({
    queryKey: ["metrics"],
    staleTime: Infinity,
    queryFn: async () => {
      const { metrics } = await api<{ metrics: Metric[] }>("/v1/metrics");
      return metrics;
    },
  });
}

/** useMetricMap returns the Catalog keyed by slug for O(1) lookup of a Metric's
 *  unit and aggregation rule. */
export function useMetricMap() {
  const query = useMetrics();
  const map = new Map<string, Metric>();
  for (const m of query.data ?? []) map.set(m.slug, m);
  return { ...query, map };
}
