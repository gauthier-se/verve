import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";

import { createAppRouter } from "@/router";

/**
 * Renders the real application router at `path`, with a fresh QueryClient
 * that does not retry failed queries.
 */
export function renderRoute(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createAppRouter({
    queryClient,
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  const result = render(<RouterProvider router={router} />);

  return { ...result, queryClient, router };
}
