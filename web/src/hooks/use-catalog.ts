import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Activity, Metric } from "@/lib/types";

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

/** useActivityMap returns the Activity display table keyed by slug, from the same
 *  cached Catalog request. It is what lets a chart label a breakdown segment:
 *  training volume stacks a bucket by Activity and the keys arrive as bare slugs,
 *  while the curated labels live on the server (ADR 0002, ADR 0040). */
export function useActivityMap() {
  const query = useQuery({
    queryKey: ["metrics"],
    staleTime: Infinity,
    queryFn: async () => {
      const { activities } = await api<{ metrics: Metric[]; activities: Activity[] }>("/v1/metrics");
      return activities;
    },
  });
  const map = new Map<string, Activity>();
  for (const a of query.data ?? []) map.set(a.slug, a);
  return map;
}
