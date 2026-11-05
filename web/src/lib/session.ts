import type { createAppRouter } from "@/router";

type AppRouter = ReturnType<typeof createAppRouter>;

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
