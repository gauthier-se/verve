import { QueryClient, useQueryClient } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";

import { createAppRouter } from "@/router";

describe("createAppRouter", () => {
  it("given a QueryClient, when the router loads a route, then every matched route context exposes that QueryClient", async () => {
    const queryClient = new QueryClient();
    const router = createAppRouter({
      queryClient,
      history: createMemoryHistory({ initialEntries: ["/"] }),
    });

    await router.load();

    expect(router.state.matches).not.toHaveLength(0);
    for (const match of router.state.matches) {
      expect(match.context.queryClient).toBe(queryClient);
    }
  });

  it("given a QueryClient, when a component inside the router calls useQueryClient, then it gets that QueryClient", async () => {
    const queryClient = new QueryClient();
    const router = createAppRouter({
      queryClient,
      history: createMemoryHistory({ initialEntries: ["/"] }),
    });
    function QueryClientProbe({ children }: { children: ReactNode }) {
      const client = useQueryClient();
      return (
        <>
          <output>
            {client === queryClient ? "same client" : "other client"}
          </output>
          {children}
        </>
      );
    }

    render(<RouterProvider router={router} InnerWrap={QueryClientProbe} />);

    expect(await screen.findByRole("status")).toHaveTextContent("same client");
  });
});
