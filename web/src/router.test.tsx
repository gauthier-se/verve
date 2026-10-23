import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
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
});
