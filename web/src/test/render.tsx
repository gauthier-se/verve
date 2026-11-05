import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";

import { createAppRouter } from "@/router";

/**
 * Renders the real application router at `path`, with a fresh QueryClient
 * that does not retry failed queries.
 */
export function renderRoute(path: string) {
  const router = createAppRouter({
    history: createMemoryHistory({ initialEntries: [path] }),
    queryDefaults: { queries: { retry: false } },
  });
  const { queryClient } = router.options.context;

  const result = render(<RouterProvider router={router} />);

  return { ...result, queryClient, router };
}
