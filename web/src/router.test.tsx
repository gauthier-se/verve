import { useQueryClient } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";

import { createAppRouter } from "@/router";

describe("createAppRouter", () => {
  it("given the router, when it loads a route, then every matched route context exposes the QueryClient of the router", async () => {
    const router = createAppRouter({
      history: createMemoryHistory({ initialEntries: ["/login"] }),
    });

    await router.load();

    expect(router.state.matches).not.toHaveLength(0);
    for (const match of router.state.matches) {
      expect(match.context.queryClient).toBe(
        router.options.context.queryClient,
      );
    }
  });

  it("given the router, when a component inside it calls useQueryClient, then it gets the QueryClient of the router", async () => {
    const router = createAppRouter({
      history: createMemoryHistory({ initialEntries: ["/login"] }),
    });
    function QueryClientProbe({ children }: { children: ReactNode }) {
      const client = useQueryClient();
      return (
        <>
          <output>
            {client === router.options.context.queryClient
              ? "same client"
              : "other client"}
          </output>
          {children}
        </>
      );
    }

    render(<RouterProvider router={router} InnerWrap={QueryClientProbe} />);

    expect(await screen.findByRole("status")).toHaveTextContent("same client");
  });
});
