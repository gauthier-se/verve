import { QueryClientProvider } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";

import { routeTree } from "@/routeTree.gen";
import type { RouterContext } from "@/routes/__root";

interface CreateAppRouterOptions extends RouterContext {
  /** Defaults to the browser history. Tests pass a memory history. */
  history?: RouterHistory;
}

/**
 * Creates the application router. The QueryClient is available both in the
 * route context (for loaders) and through QueryClientProvider (for hooks).
 */
export function createAppRouter({
  history,
  ...context
}: CreateAppRouterOptions) {
  return createRouter({
    routeTree,
    context,
    ...(history && { history }),
    Wrap: ({ children }) => (
      <QueryClientProvider client={context.queryClient}>
        {children}
      </QueryClientProvider>
    ),
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
