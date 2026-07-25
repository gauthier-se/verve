import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { BodyCompositionTrust, Phase, Plan, Profile } from "@/lib/types";

const PLAN = ["plan"];
const PHASES = ["phases"];
const PROFILE = ["profile"];

/** useInvalidate refreshes the Plan and everything that feeds it. Opening a Phase or
 *  editing the profile changes what /v1/plan derives, so the plan cache must go with them. */
function useInvalidate() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: PLAN });
    void qc.invalidateQueries({ queryKey: PHASES });
    void qc.invalidateQueries({ queryKey: PROFILE });
  };
}

/** usePlan loads the whole Plan page in one call. `rate` previews a Target rate without
 *  committing to it — what the slider sends while it is being dragged. Every figure is
 *  derived server-side; the client renders and never recomputes (ADR 0019, ADR 0023).
 *
 *  `placeholderData` keeps the previous payload on screen while a new rate is in flight,
 *  so dragging the slider updates the numbers instead of flashing a spinner over them. */
export function usePlan(rate?: number) {
  return useQuery({
    queryKey: [...PLAN, rate ?? null],
    placeholderData: (previous) => previous,
    queryFn: async () => {
      const qs = rate === undefined ? "" : `?rate=${rate}`;
      const { plan } = await api<{ plan: Plan }>(`/v1/plan${qs}`);
      return plan;
    },
  });
}

/** usePhases loads the Phase history, newest first. */
export function usePhases() {
  return useQuery({
    queryKey: PHASES,
    queryFn: async () => {
      const { phases } = await api<{ phases: Phase[] }>("/v1/phases");
      return phases;
    },
  });
}

/** useOpenPhase starts a Phase at the given rate, closing whatever was open. */
export function useOpenPhase() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (rate: number) =>
      api<{ phase: Phase }>("/v1/phases", { method: "POST", body: { rate_pct_per_week: rate } }),
    onSuccess: invalidate,
  });
}

/** useClosePhase ends an open Phase without starting another — stepping off a plan rather
 *  than switching to a new one. */
export function useClosePhase() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api<{ phase: Phase }>(`/v1/phases/${id}`, { method: "PATCH", body: {} }),
    onSuccess: invalidate,
  });
}

export function useProfile() {
  return useQuery({
    queryKey: PROFILE,
    queryFn: async () => {
      const { profile } = await api<{ profile: Profile }>("/v1/profile");
      return profile;
    },
  });
}

export interface ProfilePatch {
  date_of_birth?: string | null;
  biological_sex?: "male" | "female" | null;
  body_composition_trust?: BodyCompositionTrust | null;
}

/** useUpdateProfile patches the Account attributes that are not Measurements. Fields left
 *  out are untouched; height, mass and body fat go through Manual entry instead. */
export function useUpdateProfile() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (patch: ProfilePatch) =>
      api<{ profile: Profile }>("/v1/profile", { method: "PATCH", body: patch }),
    onSuccess: invalidate,
  });
}
