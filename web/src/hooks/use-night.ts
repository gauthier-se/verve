import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { NightDetail } from "@/lib/types";

/** useNight loads one Night as an entity (GET /v1/nights/{date}, ADR 0041): the
 *  intervals it was folded from and the figures a bucket destroys. It is keyed
 *  by the Night label, the morning it woke on, which is what every sleep Point
 *  already carries. */
export function useNight(date: string) {
  return useQuery({
    queryKey: ["night", date],
    queryFn: () => api<{ night: NightDetail }>(`/v1/nights/${date}`).then((r) => r.night),
    // A night that was never recorded is a 404 and stays one: retrying asks the
    // same question of the same rows.
    retry: false,
  });
}
