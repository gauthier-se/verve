import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { History } from "@/lib/types";

/** useHistory fetches the long view (GET /v1/history): one Metric drawn over the
 *  Account's entire span, and the dated events that explain its shape.
 *
 *  One call, not four. The band's grain, the Phases folded onto it, the gaps and
 *  the events all have to agree about the same axis, and assembling that agreement
 *  from separate endpoints would mean deriving it here (ADR 0012). */
export function useHistory(metric?: string) {
  return useQuery({
    queryKey: ["history", metric ?? null],
    queryFn: async (): Promise<History> => {
      const qs = metric ? `?metric=${encodeURIComponent(metric)}` : "";
      const raw = await api<{ history: History }>(`/v1/history${qs}`);
      return raw.history;
    },
  });
}
