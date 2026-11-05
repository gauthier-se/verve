import { act, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  getCurrentUserQueryKey,
  listAccountAuditEventsOptions,
} from "@/api/generated/@tanstack/react-query.gen";
import { renderRoute } from "@/test/render";
import { admin, fakeAuth, member, server, unauthorized } from "@/test/server";

describe("protected routes", () => {
  it.each([
    { path: "/", redirect: "/" },
    { path: "/?period=week", redirect: "/?period=week" },
    { path: "/admin", redirect: "/admin" },
  ])(
    "given no session, when opening $path, then the app goes to the login page with $redirect as the target",
    async ({ path, redirect }) => {
      server.use(...fakeAuth({ user: member }));
      const { router } = renderRoute(path);

      expect(
        await screen.findByRole("heading", { level: 1, name: "Log in" }),
      ).toBeInTheDocument();
      expect(router.state.location.pathname).toBe("/login");
      expect(router.state.location.search).toEqual({ redirect });
    },
  );

  it("given setup is required, when opening a protected route, then the app goes to the setup page", async () => {
    server.use(...fakeAuth());
    const { router } = renderRoute("/admin");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Set up Verve" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/setup");
  });

  it("given a session that expires during use, when a query answers 401, then the app goes to the login page with the current path as the target", async () => {
    server.use(...fakeAuth({ user: admin, signedIn: true }));
    const { queryClient, router } = renderRoute("/admin");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Administration" }),
    ).toBeInTheDocument();
    server.use(
      ...fakeAuth({ user: admin }),
      unauthorized("/api/v1/account/audit"),
    );

    // A query of the page, for data of the account.
    await act(() =>
      queryClient.query(listAccountAuditEventsOptions()).catch(() => undefined),
    );

    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ redirect: "/admin" });
    // The data of the account does not outlive its session.
    expect(queryClient.getQueryData(getCurrentUserQueryKey())).toBeUndefined();
  });

  it("given no session, when opening an unknown route, then the app shows the not-found page", async () => {
    server.use(...fakeAuth({ user: member }));
    const { router } = renderRoute("/does-not-exist");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/does-not-exist");
  });
});
