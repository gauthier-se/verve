import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { Account, AuthState, MapConfig } from "@/lib/types";

/** MePayload is what GET /v1/auth/me returns: the Account, plus the instance
 *  settings a client needs at boot. `map` is present only when a basemap is
 *  configured, and its absence is the default (ADR 0028). */
interface MePayload {
  account: Account | null;
  map?: MapConfig;
}

async function fetchMe(): Promise<MePayload> {
  try {
    return await api<MePayload>("/v1/auth/me");
  } catch (err) {
    if (err instanceof ApiError && err.unauthenticated) return { account: null };
    throw err;
  }
}

/** useAuthState resolves whether the instance still needs its first Account, so
 *  the unauthenticated app can pick the create-account vs. login screen (ADR 0017). */
export function useAuthState() {
  return useQuery({
    queryKey: ["auth-state"],
    queryFn: () => api<AuthState>("/v1/auth/state"),
  });
}

/** useMe resolves the logged-in Account, or null when unauthenticated. It is the
 *  gate the app checks to decide between the login screen and the dashboards. */
export function useMe() {
  return useQuery({ queryKey: ["me"], queryFn: fetchMe, select: (p) => p.account });
}

/** useMapConfig resolves the configured basemap, or null when none is: the
 *  default, in which case a workout map draws its trace on a blank ground and the
 *  browser makes no outbound request (ADR 0028). It shares the `me` query, so
 *  reading it costs no extra request. */
export function useMapConfig() {
  return useQuery({ queryKey: ["me"], queryFn: fetchMe, select: (p) => p.map ?? null });
}

/** useLogin posts credentials and, on success, primes the `me` cache so the app
 *  transitions to the dashboards without a second round-trip. */
export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api<{ account: Account }>("/v1/auth/login", { method: "POST", body: input }),
    onSuccess: ({ account }) => {
      // Prime for an instant transition, then refetch: the login payload carries the
      // Account and not the instance settings, and a map configured server-side must
      // not stay invisible until the next reload.
      qc.setQueryData<MePayload>(["me"], { account });
      qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

/** useRegister runs the first-run bootstrap: it creates the first Account and,
 *  on success, primes the `me` cache from the auto-login so the app drops the
 *  visitor straight onto their seeded dashboard (ADR 0017). */
export function useRegister() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api<{ account: Account }>("/v1/auth/register", { method: "POST", body: input }),
    onSuccess: ({ account }) => {
      qc.setQueryData<MePayload>(["me"], { account });
      qc.invalidateQueries({ queryKey: ["me"] });
      qc.setQueryData<AuthState>(["auth-state"], { needs_bootstrap: false });
    },
  });
}

/** useLogout revokes the session server-side and clears all cached data. */
export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api("/v1/auth/logout", { method: "POST" }),
    onSuccess: () => {
      qc.setQueryData<MePayload>(["me"], { account: null });
      qc.clear();
    },
  });
}
