import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { WorkoutSeries } from "@/lib/types";

/** useWorkoutSeries loads one Metric's curve inside one workout (ADR 0041). The
 *  window is the workout's own and comes back with the payload, so nothing here
 *  passes a range: there is none to pass. */
export function useWorkoutSeries(sessionId: number, metric: string, enabled: boolean) {
  return useQuery({
    queryKey: ["workout-series", sessionId, metric],
    enabled,
    queryFn: () =>
      api<{ series: WorkoutSeries }>(
        `/v1/sessions/${sessionId}/series?metric=${encodeURIComponent(metric)}`,
      ).then((r) => r.series),
  });
}
