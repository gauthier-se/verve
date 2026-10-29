import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, onTestFinished, vi } from "vitest";

import { stubLocationAssign } from "@/test/location";
import { renderRoute } from "@/test/render";
import { meta, server } from "@/test/server";

/** Renders the not-found page, whose link to the home page navigates client-side. */
async function renderLoadedApp() {
  const result = renderRoute("/does-not-exist");
  const footer = await screen.findByRole("contentinfo");
  expect(await within(footer).findByText("Verve 1.0.0")).toBeInTheDocument();
  return { ...result, footer };
}

function upgradeServer() {
  server.use(
    http.get("/api/v1/meta", () =>
      HttpResponse.json({ ...meta, version: "1.1.0" }),
    ),
  );
}

describe("root route", () => {
  it("given the server was upgraded, when the window regains focus, then the next client-side navigation performs a full page load of the target URL", async () => {
    const user = userEvent.setup();
    const assign = stubLocationAssign();
    const { footer } = await renderLoadedApp();
    upgradeServer();

    act(() => {
      document.dispatchEvent(new Event("visibilitychange", { bubbles: true }));
    });
    expect(await within(footer).findByText("Verve 1.1.0")).toBeInTheDocument();
    expect(assign).not.toHaveBeenCalled();
    await user.click(screen.getByRole("link", { name: "Back to home" }));

    expect(assign).toHaveBeenCalledExactlyOnceWith("/");
  });

  it("given the server was upgraded, when 5 minutes pass, then the next client-side navigation performs a full page load of the target URL", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const assign = stubLocationAssign();
    const { footer } = await renderLoadedApp();
    upgradeServer();

    await vi.advanceTimersByTimeAsync(4 * 60_000);
    expect(within(footer).getByText("Verve 1.0.0")).toBeInTheDocument();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(await within(footer).findByText("Verve 1.1.0")).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "Back to home" }));

    expect(assign).toHaveBeenCalledExactlyOnceWith("/");
  });

  it("given the server version did not change, when the user navigates, then the navigation stays client-side", async () => {
    const user = userEvent.setup();
    const assign = stubLocationAssign();
    const { router } = await renderLoadedApp();

    await user.click(screen.getByRole("link", { name: "Back to home" }));

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
    expect(assign).not.toHaveBeenCalled();
  });
});
