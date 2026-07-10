import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { Account, AuthState } from "@/lib/types";

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
  return useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        const { account } = await api<{ account: Account }>("/v1/auth/me");
        return account;
      } catch (err) {
        if (err instanceof ApiError && err.unauthenticated) return null;
        throw err;
      }
    },
  });
}

/** useLogin posts credentials and, on success, primes the `me` cache so the app
 *  transitions to the dashboards without a second round-trip. */
export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api<{ account: Account }>("/v1/auth/login", { method: "POST", body: input }),
    onSuccess: ({ account }) => {
      qc.setQueryData(["me"], account);
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
      qc.setQueryData(["me"], account);
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
      qc.setQueryData(["me"], null);
      qc.clear();
    },
  });
}
