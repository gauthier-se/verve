import type { QueryClient } from "@tanstack/react-query";

import type { CurrentUser } from "@/api/generated";
import { getCurrentUserQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import type { createAppRouter } from "@/router";

type AppRouter = ReturnType<typeof createAppRouter>;

/**
 * Starts the session of `user` in the app, once a login or setup opened it:
 * the query cache keeps only `user`, as GET /api/v1/auth/me answers them. A
 * session can end without the cache being cleared, when the protected routes
 * find it gone: no data of that account may reach this one.
 */
export function enterSession(queryClient: QueryClient, user: CurrentUser) {
  queryClient.removeQueries();
  queryClient.setQueryData(getCurrentUserQueryKey(), user);
}

/**
 * Ends the session in the app: goes to the login page, with `redirect` as the
 * target after login, then clears the query cache, so that no data of the
 * account outlives its session. Leaving the protected page first stops its
 * queries from refetching. The login page replaces the page in the history,
 * which needs a session from now on.
 */
export async function leaveSession(router: AppRouter, redirect?: string) {
  await router.navigate({
    to: "/login",
    search: { redirect },
    replace: true,
  });
  router.options.context.queryClient.clear();
}
