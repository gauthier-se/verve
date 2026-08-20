import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Activity, RouteGeometry, SessionDetail, SessionPage } from "@/lib/types";
import type { RangeTokens } from "@/lib/time-range";

/** SessionFilter is what the Workouts page asks for: its own window and its own
 *  activity groups, both independent of any Dashboard's (ADR 0028). */
export interface SessionFilter {
  range: RangeTokens;
  groups: Activity["group"][];
}

function params(filter: SessionFilter, cursor?: string): string {
  const qs = new URLSearchParams();
  qs.set("range_preset", filter.range.preset);
  if (filter.range.preset === "custom") {
    if (filter.range.from) qs.set("range_from", filter.range.from);
    if (filter.range.to) qs.set("range_to", filter.range.to);
  }
  for (const g of filter.groups) qs.append("group", g);
  if (cursor) qs.set("cursor", cursor);
  return qs.toString();
}

/** useSessions loads the workout list one page at a time, following the server's
 *  cursor. The cursor is a keyset and not an offset, so an import running while
 *  someone scrolls cannot make a row appear twice or vanish. */
export function useSessions(filter: SessionFilter) {
  return useInfiniteQuery({
    queryKey: ["sessions", filter.range.preset, filter.range.from, filter.range.to, filter.groups],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => api<SessionPage>(`/v1/sessions?${params(filter, pageParam)}`),
    getNextPageParam: (last) => last.next_cursor,
  });
}

/** useSession loads one workout: its figures, its stats and its route references.
 *  The geometry is a separate request, so opening a workout does not parse a file
 *  the page may not draw. */
export function useSession(id: number) {
  return useQuery({
    queryKey: ["session", id],
    queryFn: () => api<SessionDetail>(`/v1/sessions/${id}`),
  });
}

/** useSessionRoutes loads a workout's geometry: every Route simplified, with its
 *  profiles. Several Routes stay several; joining them would draw a line across
 *  ground nobody covered (ADR 0028). */
export function useSessionRoutes(id: number, enabled: boolean) {
  return useQuery({
    queryKey: ["session-routes", id],
    queryFn: async () => {
      const { routes } = await api<{ routes: RouteGeometry[] }>(`/v1/sessions/${id}/routes`);
      return routes;
    },
    enabled,
  });
}
