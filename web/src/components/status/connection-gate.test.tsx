import {
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, onTestFinished, vi } from "vitest";

import { ConnectionGate } from "@/components/status/connection-gate";
import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { meta, server } from "@/test/server";

/**
 * Renders the gate around a placeholder layout, with a QueryClient using the
 * default options: the gate must not rely on the retries of the QueryClient.
 */
function renderGate() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <ConnectionGate>
        <p>Layout</p>
      </ConnectionGate>
    </QueryClientProvider>,
  );
}

/** Answers GET /api/v1/meta with `resolver`, counting the requests. */
function answerMeta(resolver: () => Response) {
  const counter = { requests: 0 };
  server.use(
    http.get("/api/v1/meta", () => {
      counter.requests += 1;
      return resolver();
    }),
  );
  return counter;
}

const networkError = () => HttpResponse.error();
const starting = () =>
  problemResponse({
    title: "Service Unavailable",
    status: 503,
    code: "starting",
  });

describe("ConnectionGate", () => {
  it.each([
    { failure: "a network error", resolver: networkError },
    {
      failure: "a 502 from a reverse proxy",
      resolver: () =>
        new HttpResponse("Bad Gateway", {
          status: 502,
          headers: { "Content-Type": "text/plain" },
        }),
    },
    {
      failure: "a 504 from a reverse proxy",
      resolver: () => new HttpResponse(null, { status: 504 }),
    },
    {
      failure: "a 503 from a reverse proxy whose backend is down",
      resolver: () =>
        new HttpResponse("no available server", {
          status: 503,
          headers: { "Content-Type": "text/plain" },
        }),
    },
  ])(
    "given /api/v1/meta fails with $failure, then a server unreachable screen replaces the layout with a retry button",
    async ({ resolver }) => {
      answerMeta(resolver);

      renderGate();

      const status = await screen.findByRole("status");
      expect(status).toHaveAttribute("aria-live", "polite");
      expect(
        within(status).getByRole("heading", {
          level: 1,
          name: "Server unreachable",
        }),
      ).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
      expect(screen.queryByText("Layout")).not.toBeInTheDocument();
    },
  );

  it("given the browser is offline, when the app loads, then the server unreachable screen shows instead of waiting for the network", async () => {
    onlineManager.setOnline(false);
    onTestFinished(() => {
      onlineManager.setOnline(true);
    });
    answerMeta(networkError);

    renderGate();

    expect(
      await screen.findByRole("heading", { name: "Server unreachable" }),
    ).toBeInTheDocument();
  });

  it("given the server unreachable screen, when the user tabs to the retry button and presses Enter, then the app retries at once and restores the layout", async () => {
    const user = userEvent.setup();
    answerMeta(networkError);
    renderGate();
    const retry = await screen.findByRole("button", { name: "Retry" });

    await user.tab();
    expect(retry).toHaveFocus();
    answerMeta(() => HttpResponse.json(meta));
    await user.keyboard("{Enter}");

    expect(await screen.findByText("Layout")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it('given /api/v1/meta returns 503 with code "starting", then a Verve is starting screen replaces the layout', async () => {
    answerMeta(starting);

    renderGate();

    const status = await screen.findByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(
      within(status).getByRole("heading", {
        level: 1,
        name: "Verve is starting",
      }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Layout")).not.toBeInTheDocument();
  });

  it.each([
    {
      screen: "server unreachable",
      resolver: networkError,
      title: "Server unreachable",
    },
    { screen: "starting", resolver: starting, title: "Verve is starting" },
  ])(
    "given the $screen screen, when the API answers again, then the app retries every 5 seconds and restores the layout without reload",
    async ({ resolver, title }) => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      onTestFinished(() => {
        vi.useRealTimers();
      });
      const failing = answerMeta(resolver);
      renderGate();
      expect(await screen.findByRole("heading", { name: title })).toBeVisible();

      await vi.advanceTimersByTimeAsync(4_000);
      expect(failing.requests).toBe(1);
      await vi.advanceTimersByTimeAsync(1_000);
      await waitFor(() => {
        expect(failing.requests).toBe(2);
      });
      expect(screen.getByRole("heading", { name: title })).toBeVisible();

      const answering = answerMeta(() => HttpResponse.json(meta));
      await vi.advanceTimersByTimeAsync(5_000);

      expect(await screen.findByText("Layout")).toBeInTheDocument();
      expect(answering.requests).toBe(1);
      expect(screen.queryByRole("status")).not.toBeInTheDocument();
    },
  );

  it("given the app in French, when the server is unreachable, then the screen and its retry button are in French", async () => {
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    answerMeta(networkError);

    renderGate();

    expect(
      await screen.findByRole("heading", { name: "Serveur injoignable" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Réessayer" }),
    ).toBeInTheDocument();
  });

  it("given the app in French, when the server is starting, then the screen is in French", async () => {
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    answerMeta(starting);

    renderGate();

    expect(
      await screen.findByRole("heading", { name: "Verve démarre" }),
    ).toBeInTheDocument();
  });

  it("given /api/v1/meta answers, then the gate renders the layout", async () => {
    renderGate();

    expect(await screen.findByText("Layout")).toBeInTheDocument();
  });
});
