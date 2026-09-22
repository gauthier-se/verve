import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Now, NowCard } from "@/lib/types";

/** The query key the Now read shares, so every write that can move a Latest value,
 *  a Pin, a Goal or a last datum can stale it without importing this module's
 *  internals. */
export const NOW_KEY = ["now"];

/** useNow loads the Now screen (GET /v1/now, ADR 0045): the Account's Freshness and
 *  one card per Pin. One call, because which day the data stops on, which Source is
 *  retired and which week a Goal is counted over are read-path decisions, and
 *  assembling them here would put a second clock in the browser. */
export function useNow() {
  return useQuery({
    queryKey: NOW_KEY,
    queryFn: () => api<{ now: Now }>("/v1/now").then((r) => r.now),
  });
}

/** useNowUnusual loads the followed Metrics outside their Usual (GET
 *  /v1/now/unusual, ADR 0049), apart from the rest of Now because it reads a Usual
 *  per Metric. Its key sits under NOW_KEY, so every write that stales Now stales it. */
export function useNowUnusual() {
  return useQuery({
    queryKey: [...NOW_KEY, "unusual"],
    queryFn: () => api<{ unusual: NowCard[] }>("/v1/now/unusual").then((r) => r.unusual),
  });
}
