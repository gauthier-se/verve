import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { ManualMeasurement } from "@/lib/types";

const KEY = ["measurements", "manual"];

/** useManualMeasurements loads the Account's Manual entries, newest first — the only
 *  Measurements it can delete (ADR 0022). Imported rows are never listed here: they are
 *  served aggregated, through the Series layer (ADR 0012, ADR 0021). */
export function useManualMeasurements() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const { measurements } = await api<{ measurements: ManualMeasurement[] }>(
        "/v1/measurements?source=manual",
      );
      return measurements;
    },
  });
}

export interface ManualEntryInput {
  metric: string;
  /** value is the canonical stored value, already rescaled by the caller for a `%`
   *  Metric (0.27, never 27) — the API stores exactly what it is given. */
  value: number;
  measured_at?: string;
}

/** useInvalidate refreshes everything a Manual entry can move. Writing one changes the
 *  resolved row set behind the graphs too (the Manual overlay), so the Series and Ledger
 *  caches go as stale as the entry list itself — invalidating only the list would leave
 *  the charts showing the very value that was just corrected. */
function useInvalidate() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: KEY });
    void qc.invalidateQueries({ queryKey: ["series"] });
    void qc.invalidateQueries({ queryKey: ["ledger"] });
  };
}

/** useCreateManualMeasurement records one Manual entry. The API is idempotent by content
 *  key, so a double submit is a no-op rather than a duplicate row. */
export function useCreateManualMeasurement() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (input: ManualEntryInput) =>
      api<{ measurement: ManualMeasurement }>("/v1/measurements", { method: "POST", body: input }),
    onSuccess: invalidate,
  });
}

/** useDeleteManualMeasurement removes one Manual entry. The server refuses (403) any row
 *  that is not Manual, so this can never touch imported data. */
export function useDeleteManualMeasurement() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api<void>(`/v1/measurements/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}
