import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api";

/** queryClient tunes retry so an auth failure (401) fails fast to the login
 *  screen instead of being retried, while transient errors still get one retry. */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (failureCount, error) => {
        if (error instanceof ApiError && (error.unauthenticated || error.status === 422)) {
          return false;
        }
        return failureCount < 1;
      },
      refetchOnWindowFocus: false,
    },
  },
});
