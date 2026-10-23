import { http, HttpResponse } from "msw";
import { describe, expect, it, onTestFinished, vi } from "vitest";

import { server } from "@/test/server";

describe("MSW server", () => {
  it("given a handler for GET /api/ping, when the test fetches /api/ping, then it receives the mocked response", async () => {
    server.use(http.get("/api/ping", () => HttpResponse.json({ pong: true })));

    const response = await fetch("/api/ping");

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ pong: true });
  });

  it("given no handler for a request, when the test fetches it, then the request fails with an MSW error", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {
      // Silence the expected MSW error output.
    });
    onTestFinished(() => {
      consoleError.mockRestore();
    });

    await expect(fetch("/api/unhandled")).rejects.toThrow();
    expect(consoleError).toHaveBeenCalledWith(
      expect.stringContaining("without a matching request handler"),
    );
  });
});
