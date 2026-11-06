import {
  matchQuery,
  type DefaultOptions,
  type Query,
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import {
  createBrowserHistory,
  createRouter,
  type RouterHistory,
} from "@tanstack/react-router";

import { isUnauthorized } from "@/api/errors";
import { getCurrentUserQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import { createFragmentSecretReader } from "@/lib/fragment-secret";
import { leaveSession } from "@/lib/session";
import { routeTree } from "@/routeTree.gen";

interface CreateAppRouterOptions {
  /** Defaults to the browser history. Tests pass a memory history. */
  history?: RouterHistory;
  /** Defaults to the defaults of TanStack Query. Tests turn off retries. */
  queryDefaults?: DefaultOptions;
}

/**
 * Creates the application router with its QueryClient, which is available
 * both in the route context (for loaders) and through QueryClientProvider (for
 * hooks), and the reader of the secrets that links carry in their fragment. A
 * query that answers 401 means that the session ended during use: the app
 * leaves the session, with the current URL as the target after login.
 */
export function createAppRouter({
  history = createBrowserHistory(),
  queryDefaults,
}: CreateAppRouterOptions = {}) {
  const queryClient: QueryClient = new QueryClient({
    defaultOptions: {
      ...queryDefaults,
      queries: {
        // Retrying a 401 would only delay the login page.
        retry: (failureCount, error) =>
          !isUnauthorized(error) && failureCount < 3,
        ...queryDefaults?.queries,
      },
    },
    queryCache: new QueryCache({
      onError: (error, query) => {
        const { href, pathname } = router.state.location;
        // Other queries of the page can answer 401 too.
        if (
          isUnauthorized(error) &&
          !isSessionCheck(query) &&
          pathname !== "/login"
        ) {
          void leaveSession(router, href);
        }
      },
    }),
  });

  const router = createRouter({
    routeTree,
    context: {
      queryClient,
      readFragmentSecret: createFragmentSecretReader(history),
    },
    history,
    Wrap: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });

  return router;
}

/**
 * Tells whether a query is the first check of the session by the protected
 * routes, which go to the login page themselves when it answers 401.
 */
function isSessionCheck(query: Query<unknown, unknown, unknown>): boolean {
  return (
    query.state.data === undefined &&
    matchQuery({ queryKey: getCurrentUserQueryKey(), exact: true }, query)
  );
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
