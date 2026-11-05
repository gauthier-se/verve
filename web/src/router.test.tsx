import { QueryObserver, useQueryClient } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { http } from "msw";
import type { ReactNode } from "react";
import { describe, expect, it, onTestFinished } from "vitest";

import { listAccountAuditEventsOptions } from "@/api/generated/@tanstack/react-query.gen";
import { createAppRouter } from "@/router";
import { problemResponse } from "@/test/problem";
import { admin, fakeAuth, server } from "@/test/server";

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

  it("given the default query options, when a query of a page answers 401, then the app goes to the login page without retrying it", async () => {
    const requests: Request[] = [];
    server.use(...fakeAuth({ user: admin, signedIn: true }));
    const router = createAppRouter({
      history: createMemoryHistory({ initialEntries: ["/admin"] }),
    });
    render(<RouterProvider router={router} />);
    expect(
      await screen.findByRole("heading", { level: 1, name: "Administration" }),
    ).toBeInTheDocument();
    server.use(
      ...fakeAuth({ user: admin }),
      http.get("/api/v1/account/audit", ({ request }) => {
        requests.push(request);
        return problemResponse({
          title: "Unauthorized",
          status: 401,
          code: "unauthorized",
        });
      }),
    );

    // Observes the query as useQuery does, with the retries of a component.
    const observer = new QueryObserver(
      router.options.context.queryClient,
      listAccountAuditEventsOptions(),
    );
    const unsubscribe = observer.subscribe(() => undefined);
    onTestFinished(unsubscribe);

    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(requests).toHaveLength(1);
  });
});
