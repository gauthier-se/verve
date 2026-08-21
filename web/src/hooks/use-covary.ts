import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { RangeTokens } from "@/lib/time-range";
import type { CoVary, CoVaryLag } from "@/lib/types";

/** useCoVary fetches the cross-metric read (GET /v1/covary): the Account's pinned
 *  Metrics paired over one window at one lag.
 *
 *  Everything on that page is computed server-side — the coefficients, the ranking,
 *  the threshold, the fitted line — so this hook fetches an answer rather than
 *  ingredients. The page draws it (ADR 0012). */
export function useCoVary(params: { range: RangeTokens; lag: CoVaryLag }) {
  const { range, lag } = params;
  return useQuery({
    queryKey: ["covary", range.preset, range.from, range.to, lag],
    queryFn: async (): Promise<CoVary> => {
      const qs = new URLSearchParams({ range_preset: range.preset, lag });
      if (range.preset === "custom") {
        if (range.from) qs.set("range_from", range.from);
        if (range.to) qs.set("range_to", range.to);
      }
      const raw = await api<{ covary: CoVary }>(`/v1/covary?${qs.toString()}`);
      return raw.covary;
    },
  });
}
