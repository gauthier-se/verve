import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { DAY_KEY } from "./use-day";
import type { Goal, GoalDirection } from "@/lib/types";

const KEY = ["goals"];

/** GoalInput is what setting a Goal takes. `value` is canonical (minutes for sleep, a
 *  fraction for a percent); `started_on` absent means today, on the server's clock. */
export interface GoalInput {
  metric: string;
  direction: GoalDirection;
  value: number;
  started_on?: string;
}

/** useGoals loads one Metric's Goal history, newest first (ADR 0044). */
export function useGoals(metric: string) {
  return useQuery({
    queryKey: [...KEY, metric],
    queryFn: async () => {
      const { goals } = await api<{ goals: Goal[] }>(`/v1/goals?metric=${encodeURIComponent(metric)}`);
      return goals;
    },
  });
}

/** useInvalidate refreshes everything a Goal is read on. A Goal changes no data, but
 *  every Series carries the Attainment of the Goals in force over its window, and the
 *  Day prints the bound beside the value, so both go with the history. */
function useInvalidate() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: KEY });
    void qc.invalidateQueries({ queryKey: ["series"] });
    void qc.invalidateQueries({ queryKey: DAY_KEY });
  };
}

/** useSetGoal sets a Goal, closing the one open on that Metric. It never edits the
 *  previous one: each day is judged against the Goal in force on it. */
export function useSetGoal() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (input: GoalInput) => api<{ goal: Goal }>("/v1/goals", { method: "POST", body: input }),
    onSuccess: invalidate,
  });
}

/** useCloseGoal ends an open Goal today without setting another. */
export function useCloseGoal() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api<{ goal: Goal }>(`/v1/goals/${id}`, { method: "PATCH", body: {} }),
    onSuccess: invalidate,
  });
}

/** useDeleteGoal removes an entry outright: how a mistyped value, or the past, is
 *  corrected, since a new Goal cannot be inserted into the history. */
export function useDeleteGoal() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api(`/v1/goals/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}
