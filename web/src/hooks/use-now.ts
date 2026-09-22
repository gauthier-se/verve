import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Now } from "@/lib/types";

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
