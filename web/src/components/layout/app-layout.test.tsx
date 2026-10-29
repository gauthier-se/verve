import { screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { getMetaQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import { problemResponse } from "@/test/problem";
import { meta, server } from "@/test/server";
import { renderRoute } from "@/test/render";

describe("AppLayout", () => {
  it('given the app loads, when /api/v1/meta answers, then the footer shows "Verve <version>"', async () => {
    server.use(
      http.get("/api/v1/meta", () =>
        HttpResponse.json({ ...meta, version: "2.4.0" }),
      ),
    );

    renderRoute("/");

    const footer = await screen.findByRole("contentinfo");
    expect(await within(footer).findByText("Verve 2.4.0")).toBeInTheDocument();
  });

  it("given the app loads, then a header shows the product name linking to the home page and the main area shows the current route", async () => {
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
      screen.getByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("contentinfo")).toHaveTextContent(/^Verve$/);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
