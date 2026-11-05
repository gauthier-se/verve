import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderRoute } from "@/test/render";
import { admin, fakeAuth, member, server } from "@/test/server";

describe("admin routes", () => {
  it("given an admin, when opening /admin, then the app shows the administration page", async () => {
    server.use(...fakeAuth({ user: admin, signedIn: true }));
    renderRoute("/admin");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Administration" }),
    ).toBeInTheDocument();
  });

  it("given a member, when opening /admin, then the app shows the not-found page", async () => {
    server.use(...fakeAuth({ user: member, signedIn: true }));
    const { router } = renderRoute("/admin");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/admin");
  });
});
