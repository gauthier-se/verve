import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { client } from "@/api/generated/client.gen";
import { server } from "@/test/server";

/** Registers a handler for `method` on /api/v1/echo that records the request it receives. */
function recordRequest(method: "get" | "post" | "delete") {
  const received: Request[] = [];
  server.use(
    http[method]("/api/v1/echo", ({ request }) => {
      received.push(request);
      return HttpResponse.json({});
    }),
  );
  return received;
}

describe("API client", () => {
  it("given the generated client, then it sends requests to the same origin with same-origin credentials", () => {
    expect(client.getConfig()).toMatchObject({
      baseUrl: window.location.origin,
      credentials: "same-origin",
    });
  });

  it("given a GET request, when the client sends it, then it has no CSRF header", async () => {
    const received = recordRequest("get");

    await client.get({ url: "/api/v1/echo" });

    expect(received).toHaveLength(1);
    expect(received[0]?.url).toBe(`${window.location.origin}/api/v1/echo`);
    expect(received[0]?.headers.has("Verve-Csrf")).toBe(false);
  });

  it.each(["post", "delete"] as const)(
    "given a %s request, when the client sends it, then it carries the header Verve-Csrf: 1",
    async (method) => {
      const received = recordRequest(method);

      await client[method]({ url: "/api/v1/echo" });

      expect(received).toHaveLength(1);
      expect(received[0]?.headers.get("Verve-Csrf")).toBe("1");
    },
  );
});
