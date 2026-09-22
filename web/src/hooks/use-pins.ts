import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Pin } from "@/lib/types";
import { NOW_KEY } from "./use-now";

const KEY = ["pins"];

/** usePins loads the Metrics the Account keeps in the sidebar, in sidebar order.
 *  A Pin is a shortcut to a Metric page and carries no time axis (ADR 0025), so
 *  there is nothing here to configure and nothing to patch. */
export function usePins() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const { pins } = await api<{ pins: Pin[] }>("/v1/pins");
      return pins;
    },
  });
}

function useInvalidate() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: KEY });
    // A Pin is a card on Now (ADR 0045).
    void qc.invalidateQueries({ queryKey: NOW_KEY });
  };
}

/** useAddPin pins a Metric. The server is idempotent, so the caller never has to
 *  check whether it is already pinned. */
export function useAddPin() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (metric: string) =>
      api<{ pin: Pin }>("/v1/pins", { method: "POST", body: { metric } }),
    onSuccess: invalidate,
  });
}

/** useRemovePin unpins a Metric, idempotently for the same reason. */
export function useRemovePin() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (metric: string) => api(`/v1/pins/${encodeURIComponent(metric)}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}
