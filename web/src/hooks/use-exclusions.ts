import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Exclusion, ExclusionPreview } from "@/lib/types";

const KEY = ["exclusions"];

/** ExclusionInput is what creating one takes: a Catalog slug and optional inclusive
 *  day bounds. Absent bounds mean unbounded on that side, so `{metric}` alone is
 *  "everything this Metric holds" — the common case, and the one that must not
 *  require typing two dates to express. */
export interface ExclusionInput {
  metric: string;
  starts_on?: string;
  ends_on?: string;
}

/** useExclusions loads the Account's whole set: what the next import will refuse.
 *  It is the remembered import filter, and there is no second object behind it. */
export function useExclusions() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const { exclusions } = await api<{ exclusions: Exclusion[] }>("/v1/exclusions");
      return exclusions;
    },
  });
}

/** useExclusionPreview counts what an Exclusion over this Metric and span would
 *  delete, without writing anything. The server counts it with the very predicate
 *  the purge will run, so the number on screen is the number that goes: a
 *  confirmation naming a different figure would be an estimate, not a confirmation.
 *
 *  Disabled without a Metric, and it does not retry: a 422 here means the slug is
 *  one an Exclusion cannot name, which is an answer rather than a failure. */
export function useExclusionPreview(input: ExclusionInput | null) {
  const params = new URLSearchParams({
    metric: input?.metric ?? "",
    starts_on: input?.starts_on ?? "",
    ends_on: input?.ends_on ?? "",
  });
  return useQuery({
    queryKey: ["exclusion-preview", input?.metric, input?.starts_on, input?.ends_on],
    queryFn: () => api<ExclusionPreview>(`/v1/exclusions/preview?${params}`),
    enabled: Boolean(input?.metric),
    retry: false,
  });
}

/** useInvalidate refreshes everything a purge just changed.
 *
 *  This is the widest invalidation in the app, and deliberately: creating an
 *  Exclusion deletes Measurements, so every read of the data is stale at once — the
 *  curves, the tables behind them, the ledger's folded figures, the history's band
 *  and its events, and the import page's has-data flag. No other write here removes
 *  years of rows, and none of the others needs this list. */
function useInvalidate() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: KEY });
    qc.invalidateQueries({ queryKey: ["series"] });
    qc.invalidateQueries({ queryKey: ["ledger"] });
    qc.invalidateQueries({ queryKey: ["history"] });
    qc.invalidateQueries({ queryKey: ["measurements"] });
    qc.invalidateQueries({ queryKey: ["import-status"] });
    qc.invalidateQueries({ queryKey: ["exclusion-preview"] });
  };
}

/** useCreateExclusion writes the rule and purges what it covers, in one call. The
 *  server answers 200 rather than a conflict for a rule that already stands, and
 *  reports the original purge count rather than a fresh one, so a double submit
 *  cannot read as a second deletion. */
export function useCreateExclusion() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: async (input: ExclusionInput) => {
      const { exclusion } = await api<{ exclusion: Exclusion }>("/v1/exclusions", {
        method: "POST",
        body: input,
      });
      return exclusion;
    },
    onSuccess: invalidate,
  });
}

/** useRemoveExclusion deletes the rule and only the rule. Nothing is restored here:
 *  the next import brings back whatever the export still holds, which is this
 *  feature's only undo and is worth saying on screen. */
export function useRemoveExclusion() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api(`/v1/exclusions/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}
