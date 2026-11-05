import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { getMetaQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import { problemResponse } from "@/test/problem";
import { admin, fakeAuth, member, meta, server } from "@/test/server";
import { renderRoute } from "@/test/render";

describe("AppLayout", () => {
  it('given the app loads, when /api/v1/meta answers, then the footer shows "Verve <version>"', async () => {
    server.use(
      ...fakeAuth({ user: member, signedIn: true }),
      http.get("/api/v1/meta", () =>
        HttpResponse.json({ ...meta, version: "2.4.0" }),
      ),
    );

    renderRoute("/");

    const footer = await screen.findByRole("contentinfo");
    expect(await within(footer).findByText("Verve 2.4.0")).toBeInTheDocument();
  });

  it("given the app loads, then a header shows the product name linking to the home page and the main area shows the current route", async () => {
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    const header = await screen.findByRole("banner");
    expect(within(header).getByRole("link", { name: "Verve" })).toHaveAttribute(
      "href",
      "/",
    );
    const main = screen.getByRole("main");
    expect(
      await within(main).findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
  });

  it("given /api/v1/meta fails with an error other than a connection failure, then the layout is shown with the product name alone in the footer", async () => {
    server.use(
      ...fakeAuth({ user: member, signedIn: true }),
      http.get("/api/v1/meta", () =>
        problemResponse({
          title: "Internal Server Error",
          status: 500,
          code: "internal",
        }),
      ),
    );

    const { queryClient } = renderRoute("/");

    await waitFor(() => {
      expect(queryClient.getQueryState(getMetaQueryKey())?.status).toBe(
        "error",
      );
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("contentinfo")).toHaveTextContent(/^Verve$/);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("given a public page, then the header shows neither the navigation nor a user menu, and the footer stays visible", async () => {
    renderRoute("/login");

    const footer = await screen.findByRole("contentinfo");
    expect(await within(footer).findByText("Verve 1.0.0")).toBeInTheDocument();
    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: member.username }),
    ).not.toBeInTheDocument();
  });

  it("given a session, then the header shows the navigation and a user menu named after the account", async () => {
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    const header = await screen.findByRole("banner");
    const navigation = await within(header).findByRole("navigation", {
      name: "Main",
    });
    expect(
      within(navigation).getByRole("link", { name: "Home" }),
    ).toHaveAttribute("href", "/");
    expect(
      within(header).getByRole("button", { name: "alice" }),
    ).toBeInTheDocument();
  });

  it("given a session, when the user opens the user menu, then it shows the account and an item to log out", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "alice" }));

    const menu = await screen.findByRole("menu");
    expect(menu).toHaveTextContent("alice@example.com");
    expect(
      within(menu).getByRole("menuitem", { name: "Log out" }),
    ).toBeInTheDocument();
  });

  it("given a session, when the user logs out from the user menu, then the app shows the login page, and a protected route asks to log in again", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: member, signedIn: true }));
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "alice" }));
    await user.click(await screen.findByRole("menuitem", { name: "Log out" }));

    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(router.state.location.href).toBe("/login");
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();

    // A cached account would let the protected route through.
    await router.navigate({ to: "/" });

    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ redirect: "/" });
  });

  it("given the logout fails, when the user logs out from the user menu, then the menu says so and the app stays on the page", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: member, signedIn: true }));
    server.use(http.post("/api/v1/auth/logout", () => HttpResponse.error()));
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "alice" }));
    await user.click(await screen.findByRole("menuitem", { name: "Log out" }));

    const menu = screen.getByRole("menu");
    expect(await within(menu).findByRole("alert")).toHaveTextContent(
      "Something went wrong. Try again.",
    );
    expect(router.state.location.pathname).toBe("/");
  });

  it("given an admin, then the navigation shows the admin section", async () => {
    server.use(...fakeAuth({ user: admin, signedIn: true }));
    renderRoute("/");

    const section = await screen.findByRole("navigation", {
      name: "Administration",
    });
    expect(
      within(section).getByRole("link", { name: "Administration" }),
    ).toHaveAttribute("href", "/admin");
  });

  it("given a member, then the navigation does not show the admin section", async () => {
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    expect(
      await screen.findByRole("navigation", { name: "Main" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("navigation", { name: "Administration" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Administration" }),
    ).not.toBeInTheDocument();
  });
});
